package main

import (
	"errors"

	"github.com/buildpacks/lifecycle"
	"github.com/buildpacks/lifecycle/buildpack"
	"github.com/buildpacks/lifecycle/cmd"
	"github.com/buildpacks/lifecycle/internal/encoding"
	"github.com/buildpacks/lifecycle/launch"
	"github.com/buildpacks/lifecycle/platform"
	"github.com/buildpacks/lifecycle/priv"
)

type buildCmd struct {
	platform Platform
	platform.BuildInputs
}

// DefineFlags defines the flags that are considered valid and reads their values (if provided).
func (b *buildCmd) DefineFlags() {
	cmd.FlagBuildpacksDir(&b.BuildpacksDir)
	cmd.FlagGroupPath(&b.GroupPath)
	cmd.FlagPlanPath(&b.PlanPath)
	cmd.FlagLayersDir(&b.LayersDir)
	cmd.FlagAppDir(&b.AppDir)
	cmd.FlagPlatformDir(&b.PlatformDir)
}

// Args validates arguments and flags, and fills in default values.
func (b *buildCmd) Args(nargs int, args []string) error {
	if nargs != 0 {
		return cmd.FailErrCode(errors.New("received unexpected arguments"), cmd.CodeInvalidArgs, "parse arguments")
	}

	var err error
	b.BuildInputs, err = b.platform.ResolveBuild(b.BuildInputs)
	if err != nil {
		return cmd.FailErrCode(err, cmd.CodeInvalidArgs, "resolve inputs")
	}
	return nil
}

func (b *buildCmd) Privileges() error {
	// builder should never be run with privileges
	if priv.IsPrivileged() {
		return cmd.FailErr(errors.New("refusing to run as root"), "build")
	}
	return nil
}

func (b *buildCmd) Exec() error {
	dirStore, err := platform.NewDirStore(b.BuildpacksDir, "")
	if err != nil {
		return err
	}
	builderFactory := lifecycle.NewBuilderFactory(
		b.platform.API(),
		&cmd.APIVerifier{},
		lifecycle.NewConfigHandler(),
		dirStore,
	)
	builder, err := builderFactory.NewBuilder(
		b.AppDir,
		buildpack.Group{},
		b.GroupPath,
		b.LayersDir,
		platform.BuildPlan{},
		b.PlanPath,
		b.PlatformDir,
		cmd.DefaultLogger,
	)
	if err != nil {
		if err, ok := err.(*cmd.ErrorFail); ok {
			return cmd.FailErrCode(err, cmd.CodeIncompatibleBuildpackAPI, "initialize builder") // TODO: add test for exit code
		}
		return cmd.FailErr(err, "initialize builder")
	}
	if _, err = doBuild(builder, buildpack.KindBuildpack, b.platform); err != nil {
		return err // pass through error
	}
	return nil
}

func doBuild(builder *lifecycle.Builder, kind string, p Platform) (platform.BuildMetadata, error) {
	md, err := builder.Build(kind)
	if err != nil {
		if err, ok := err.(*buildpack.Error); ok {
			if err.Type == buildpack.ErrTypeBuildpack {
				return platform.BuildMetadata{}, cmd.FailErrCode(err.Cause(), p.CodeFor(platform.FailedBuildWithErrors), "build")
			}
		}
		return platform.BuildMetadata{}, cmd.FailErrCode(err, p.CodeFor(platform.BuildError), "build")
	}
	if err = encoding.WriteTOML(launch.GetMetadataFilePath(builder.LayersDir), md); err != nil {
		return platform.BuildMetadata{}, cmd.FailErr(err, "write build metadata")
	}
	return *md, nil
}
