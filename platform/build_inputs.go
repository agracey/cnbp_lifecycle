package platform

import (
	"path/filepath"
)

// BuildInputs holds the values of command-line flags and args.
// Fields are the cumulative total of inputs across all supported platform APIs.
type BuildInputs struct {
	AppDir        string
	BuildpacksDir string
	// ExtensionsDir string
	// GeneratedDir  string
	GroupPath   string
	LayersDir   string
	PlanPath    string
	PlatformDir string
} // TODO: add tests

// ResolveBuild accepts a BuildInputs and returns a new BuildInputs with default values filled in,
// or an error if the provided inputs are not valid.
func (r *InputsResolver) ResolveBuild(inputs BuildInputs) (BuildInputs, error) {
	resolvedInputs := inputs

	r.fillBuildDefaultFilePaths(&resolvedInputs)

	if err := r.resolveBuildDirPaths(&resolvedInputs); err != nil {
		return BuildInputs{}, err
	}
	return resolvedInputs, nil
}

func (r *InputsResolver) fillBuildDefaultFilePaths(inputs *BuildInputs) {
	if inputs.GroupPath == PlaceholderGroupPath {
		inputs.GroupPath = defaultPath(PlaceholderGroupPath, inputs.LayersDir, r.platformAPI)
	}
	if inputs.PlanPath == PlaceholderPlanPath {
		inputs.PlanPath = defaultPath(PlaceholderPlanPath, inputs.LayersDir, r.platformAPI)
	}
}

func (r *InputsResolver) resolveBuildDirPaths(inputs *BuildInputs) error {
	var err error
	if inputs.AppDir, err = filepath.Abs(inputs.AppDir); err != nil {
		return err
	}
	if inputs.BuildpacksDir, err = filepath.Abs(inputs.BuildpacksDir); err != nil {
		return err
	}
	// if inputs.ExtensionsDir, err = absoluteIfNotEmpty(inputs.ExtensionsDir); err != nil {
	//	return err
	// }
	if inputs.LayersDir, err = filepath.Abs(inputs.LayersDir); err != nil {
		return err
	}
	if inputs.PlatformDir, err = filepath.Abs(inputs.PlatformDir); err != nil {
		return err
	}
	return nil
}
