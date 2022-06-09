package lifecycle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/buildpacks/lifecycle/api"
	"github.com/buildpacks/lifecycle/buildpack"
	"github.com/buildpacks/lifecycle/env"
	"github.com/buildpacks/lifecycle/internal/encoding"
	"github.com/buildpacks/lifecycle/internal/fsutil"
	"github.com/buildpacks/lifecycle/launch"
	"github.com/buildpacks/lifecycle/layers"
	"github.com/buildpacks/lifecycle/log"
	"github.com/buildpacks/lifecycle/platform"
)

type Builder struct {
	AppDir      string
	DirStore    DirStore
	Group       buildpack.Group // TODO: rename to Buildpacks?
	LayersDir   string
	Logger      log.Logger
	Out, Err    io.Writer // TODO: is this needed?
	Plan        platform.BuildPlan
	PlatformAPI *api.Version // TODO: remove eventually, replace with services
	PlatformDir string
}

type BuilderFactory struct {
	platformAPI   *api.Version
	apiVerifier   BuildpackAPIVerifier
	configHandler ConfigHandler
	dirStore      DirStore
}

func NewBuilderFactory(
	platformAPI *api.Version,
	apiVerifier BuildpackAPIVerifier,
	configHandler ConfigHandler,
	dirStore DirStore,
) *BuilderFactory {
	return &BuilderFactory{
		platformAPI:   platformAPI,
		apiVerifier:   apiVerifier,
		configHandler: configHandler,
		dirStore:      dirStore,
	}
}

func (f *BuilderFactory) NewBuilder(
	appDir string,
	group buildpack.Group,
	groupPath string,
	layersDir string,
	plan platform.BuildPlan,
	planPath string,
	platformDir string,
	logger log.Logger,
) (*Builder, error) {
	builder := &Builder{
		AppDir:      appDir,
		DirStore:    f.dirStore,
		LayersDir:   layersDir,
		Logger:      logger,
		PlatformDir: platformDir,
		PlatformAPI: f.platformAPI, // TODO: remove
	}
	if err := f.setGroup(builder, group, groupPath, logger); err != nil {
		return nil, err
	}
	if err := f.setPlan(builder, plan, planPath); err != nil {
		return nil, err
	}
	return builder, nil
}

func (f *BuilderFactory) setGroup(builder *Builder, group buildpack.Group, path string, logger log.Logger) error {
	if len(group.Group) > 0 {
		builder.Group = group
	} else {
		buildModules, err := f.configHandler.ReadGroup(path)
		if err != nil {
			return err
		}
		builder.Group = buildpack.Group{Group: buildModules}
	}
	for _, el := range group.Group {
		if err := f.apiVerifier.VerifyBuildpackAPI(el.Kind(), el.String(), el.API, logger); err != nil {
			return err
		}
	}
	return nil
}

func (f *BuilderFactory) setPlan(builder *Builder, plan platform.BuildPlan, path string) error {
	if len(plan.Entries) > 0 { // TODO: distinguish between empty and nil
		builder.Plan = plan
		return nil
	}
	var err error
	builder.Plan, err = f.configHandler.ReadPlan(path)
	return err
}

// TODO: see where this goes
// TODO: rename build_env.go
//go:generate mockgen -package testmock -destination testmock/env.go github.com/buildpacks/lifecycle BuildEnv
type BuildEnv interface {
	AddRootDir(baseDir string) error
	AddEnvDir(envDir string, defaultAction env.ActionType) error
	WithPlatform(platformDir string) ([]string, error)
	List() []string
}

func (b *Builder) Build(kind string) (*platform.BuildMetadata, error) {
	if kind == "" {
		kind = buildpack.KindBuildpack
	}

	// clear <layers>/sbom directory
	if err := os.RemoveAll(filepath.Join(b.LayersDir, "sbom")); err != nil {
		return nil, errors.Wrap(err, "cleaning layers SBOM directory")
	}

	// get build config
	config, err := b.BuildConfig()
	if err != nil {
		return nil, err
	}

	var (
		bomFiles  []buildpack.BOMFile
		buildBOM  []buildpack.BOMEntry
		labels    []buildpack.Label
		launchBOM []buildpack.BOMEntry
		slices    []layers.Slice
	)
	bpEnv := env.NewBuildEnv(os.Environ())
	plan := b.Plan
	processMap := newProcessMap()

	for _, groupEl := range b.Group.Filter(kind).Group {
		b.Logger.Debugf("Running build for %s %s", strings.ToLower(kind), groupEl)

		b.Logger.Debug("Looking up module")
		bpTOML, err := b.DirStore.Lookup(kind, groupEl.ID, groupEl.Version)
		if err != nil {
			return nil, err
		}

		b.Logger.Debug("Finding plan")
		bpPlan := plan.Find(groupEl.ID)

		b.Logger.Debug("Invoking build command")
		br, err := bpTOML.Build(bpPlan, config, bpEnv)
		if err != nil {
			return nil, err
		}

		b.Logger.Debug("Updating processes")
		updateDefaultProcesses(br.Processes, api.MustParse(groupEl.API), b.PlatformAPI)

		// aggregate build results
		buildBOM = append(buildBOM, br.BuildBOM...)
		launchBOM = append(launchBOM, br.LaunchBOM...)
		bomFiles = append(bomFiles, br.BOMFiles...)
		labels = append(labels, br.Labels...)
		plan = plan.Filter(br.MetRequires)
		warning := processMap.add(br.Processes)
		if warning != "" {
			b.Logger.Warn(warning)
		}
		slices = append(slices, br.Slices...)

		b.Logger.Debugf("Finished running build for %s %s", strings.ToLower(kind), groupEl)
	}

	if b.PlatformAPI.LessThan("0.4") {
		config.Logger.Debug("Updating BOM entries")
		for i := range launchBOM {
			launchBOM[i].ConvertMetadataToVersion()
		}
	}

	if b.PlatformAPI.AtLeast("0.8") {
		b.Logger.Debug("Copying SBOM files")
		err = b.copyBOMFiles(config.LayersDir, bomFiles)
		if err != nil {
			return nil, err
		}
	}

	if b.PlatformAPI.AtLeast("0.9") {
		b.Logger.Debug("Creating SBOM files for legacy BOM")
		if err := encoding.WriteJSON(filepath.Join(b.LayersDir, "sbom", "launch", "sbom.legacy.json"), launchBOM); err != nil {
			return nil, errors.Wrap(err, "encoding launch bom")
		}
		if err := encoding.WriteJSON(filepath.Join(b.LayersDir, "sbom", "build", "sbom.legacy.json"), buildBOM); err != nil {
			return nil, errors.Wrap(err, "encoding build bom")
		}
		launchBOM = []buildpack.BOMEntry{}
	}

	b.Logger.Debug("Listing processes")
	procList := processMap.list()

	b.Logger.Debug("Finished build")
	return &platform.BuildMetadata{
		BOM:                         launchBOM,
		Buildpacks:                  b.Group.Filter(buildpack.KindBuildpack).Group,
		Extensions:                  b.Group.Filter(buildpack.KindExtension).Group,
		Labels:                      labels,
		Processes:                   procList,
		Slices:                      slices,
		BuildpackDefaultProcessType: processMap.defaultType,
	}, nil
}

// copyBOMFiles() copies any BOM files written by buildpacks during the Build() process
// to their appropriate locations, in preparation for its final application layer.
// This function handles both BOMs that are associated with a layer directory and BOMs that are not
// associated with a layer directory, since "bomFile.LayerName" will be "" in the latter case.
//
// Before:
// /layers
// └── buildpack.id
//     ├── A
//     │   └── ...
//     ├── A.sbom.cdx.json
//     └── launch.sbom.cdx.json
//
// After:
// /layers
// └── sbom
//     └── launch
//         └── buildpack.id
//             ├── A
//             │   └── sbom.cdx.json
//             └── sbom.cdx.json
func (b *Builder) copyBOMFiles(layersDir string, bomFiles []buildpack.BOMFile) error {
	var (
		buildSBOMDir  = filepath.Join(layersDir, "sbom", "build")
		cacheSBOMDir  = filepath.Join(layersDir, "sbom", "cache")
		launchSBOMDir = filepath.Join(layersDir, "sbom", "launch")
		copyBOMFileTo = func(bomFile buildpack.BOMFile, sbomDir string) error {
			targetDir := filepath.Join(sbomDir, launch.EscapeID(bomFile.BuildpackID), bomFile.LayerName)
			err := os.MkdirAll(targetDir, os.ModePerm)
			if err != nil {
				return err
			}

			name, err := bomFile.Name()
			if err != nil {
				return err
			}

			return fsutil.Copy(bomFile.Path, filepath.Join(targetDir, name))
		}
	)

	for _, bomFile := range bomFiles {
		switch bomFile.LayerType {
		case buildpack.LayerTypeBuild:
			if err := copyBOMFileTo(bomFile, buildSBOMDir); err != nil {
				return err
			}
		case buildpack.LayerTypeCache:
			if err := copyBOMFileTo(bomFile, cacheSBOMDir); err != nil {
				return err
			}
		case buildpack.LayerTypeLaunch:
			if err := copyBOMFileTo(bomFile, launchSBOMDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// we set default = true for web processes when platformAPI >= 0.6 and buildpackAPI < 0.6
func updateDefaultProcesses(processes []launch.Process, buildpackAPI *api.Version, platformAPI *api.Version) {
	if platformAPI.LessThan("0.6") || buildpackAPI.AtLeast("0.6") {
		return
	}

	for i := range processes {
		if processes[i].Type == "web" {
			processes[i].Default = true
		}
	}
}

func (b *Builder) BuildConfig() (buildpack.BuildConfig, error) {
	appDir, err := filepath.Abs(b.AppDir)
	if err != nil {
		return buildpack.BuildConfig{}, err
	}
	platformDir, err := filepath.Abs(b.PlatformDir)
	if err != nil {
		return buildpack.BuildConfig{}, err
	}
	layersDir, err := filepath.Abs(b.LayersDir)
	if err != nil {
		return buildpack.BuildConfig{}, err
	}

	return buildpack.BuildConfig{
		AppDir:      appDir,
		PlatformDir: platformDir,
		LayersDir:   layersDir,
		Out:         b.Out,
		Err:         b.Err,
		Logger:      b.Logger,
	}, nil
}

type processMap struct {
	typeToProcess map[string]launch.Process
	defaultType   string
}

func newProcessMap() processMap {
	return processMap{
		typeToProcess: make(map[string]launch.Process),
		defaultType:   "",
	}
}

// This function adds the processes from listToAdd to processMap
// it sets m.defaultType to the last default process
// if a non-default process overrides a default process, it returns a warning and unset m.defaultType
func (m *processMap) add(listToAdd []launch.Process) string {
	warning := ""
	for _, procToAdd := range listToAdd {
		if procToAdd.Default {
			m.defaultType = procToAdd.Type
			warning = ""
		} else if procToAdd.Type == m.defaultType {
			// non-default process overrides a default process
			m.defaultType = ""
			warning = fmt.Sprintf("Warning: redefining the following default process type with a process not marked as default: %s\n", procToAdd.Type)
		}
		m.typeToProcess[procToAdd.Type] = procToAdd
	}
	return warning
}

// list returns a sorted array of processes.
// The array is sorted based on the process types.
// The list is sorted for reproducibility.
func (m processMap) list() []launch.Process {
	var keys []string
	for proc := range m.typeToProcess {
		keys = append(keys, proc)
	}
	sort.Strings(keys)
	result := []launch.Process{}
	for _, key := range keys {
		result = append(result, m.typeToProcess[key].NoDefault()) // we set the default to false so it won't be part of metadata.toml
	}
	return result
}
