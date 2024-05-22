package main

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	core "k8s.io/api/core/v1"
)

func perform(task *pipeline.Task, recipe *Recipe) error {
	image := "quay.io/redhat-appstudio/build-trusted-artifacts:latest@sha256:4e39fb97f4444c2946944482df47b39c5bbc195c54c6560b0647635f553ab23d"

	sourceResult := pipeline.TaskResult{
		Name:        "SOURCE_ARTIFACT",
		Description: "The Trusted Artifact URI pointing to the artifact with the application source code.",
		Type:        pipeline.ResultsTypeString,
	}

	cachi2Result := pipeline.TaskResult{
		Name:        "CACHI2_ARTIFACT",
		Description: "The Trusted Artifact URI pointing to the artifact with the prefetched dependencies.",
		Type:        pipeline.ResultsTypeString,
	}

	useSourceParam := pipeline.ParamSpec{
		Name:        "SOURCE_ARTIFACT",
		Type:        pipeline.ParamTypeString,
		Description: "The Trusted Artifact URI pointing to the artifact with the application source code.",
	}

	useCachi2Param := pipeline.ParamSpec{
		Name:        "CACHI2_ARTIFACT",
		Type:        pipeline.ParamTypeString,
		Description: "The Trusted Artifact URI pointing to the artifact with the prefetched dependencies.",
		Default:     &pipeline.ParamValue{Type: pipeline.ParamTypeString, StringVal: ""},
	}

	createParams := pipeline.ParamSpecs{
		{
			Name:        "ociStorage",
			Description: "The OCI repository where the Trusted Artifacts are stored.",
			Type:        pipeline.ParamTypeString,
		},
		{
			Name:        "ociArtifactExpiresAfter",
			Description: "Expiration date for the trusted artifacts created in the OCI repository. An empty string means the artifacts do not expire.",
			Type:        pipeline.ParamTypeString,
			Default:     &pipeline.ParamValue{Type: pipeline.ParamTypeString, StringVal: ""},
		},
	}

	task.Name += recipe.Suffix

	if recipe.Description != "" {
		task.Spec.Description = recipe.Description
	}

	if _, ok := task.Annotations["tekton.dev/displayName"]; ok {
		task.Annotations["tekton.dev/displayName"] += recipe.DisplaySuffix
	}

	task.Spec.Params = slices.DeleteFunc(task.Spec.Params, func(ps pipeline.ParamSpec) bool {
		for _, rm := range recipe.RemoveParams {
			if ps.Name == rm {
				return true
			}
		}

		return false
	})

	task.Spec.Workspaces = slices.DeleteFunc(task.Spec.Workspaces, func(wd pipeline.WorkspaceDeclaration) bool {
		for _, rm := range recipe.RemoveWorkspaces {
			if wd.Name == rm {
				return true
			}
		}
		return false
	})
	if len(task.Spec.Workspaces) == 0 {
		task.Spec.Workspaces = nil
	}

	task.Spec.Volumes = slices.DeleteFunc(task.Spec.Volumes, func(v core.Volume) bool {
		for _, rm := range recipe.RemoveVolumes {
			if v.Name == rm {
				return true
			}
		}
		return false
	})

	task.Spec.Params = append(task.Spec.Params, recipe.AddParams...)

	if recipe.useSource {
		task.Spec.Params = append(task.Spec.Params, useSourceParam)
	}

	if recipe.useCachi2 {
		task.Spec.Params = append(task.Spec.Params, useCachi2Param)
	}

	if recipe.createSource || recipe.createCachi2 {
		task.Spec.Params = append(task.Spec.Params, createParams...)
	}

	if len(recipe.AddResult) == 0 {
		if recipe.createSource {
			recipe.AddResult = append(recipe.AddResult, sourceResult)
		}
		if recipe.createCachi2 {
			recipe.AddResult = append(recipe.AddResult, cachi2Result)
		}
	}
	task.Spec.Results = append(task.Spec.Results, recipe.AddResult...)

	if len(recipe.AddVolume) == 0 {
		recipe.AddVolume = []core.Volume{{
			Name: "workdir",
			VolumeSource: core.VolumeSource{
				EmptyDir: &core.EmptyDirVolumeSource{},
			},
		}}
	}
	task.Spec.Volumes = append(task.Spec.Volumes, recipe.AddVolume...)

	workdirVolumeMount := core.VolumeMount{
		Name:      "workdir",
		MountPath: "/var/workdir",
	}
	if len(recipe.AddVolumeMount) == 0 {
		recipe.AddVolumeMount = []core.VolumeMount{workdirVolumeMount}
	}

	removeEnv := func(env *[]string) func(core.EnvVar) bool {
		return func(e core.EnvVar) bool {
			for _, rm := range recipe.RemoveParams {
				if strings.Contains(e.Value, "$(params."+rm+")") {
					*env = append(*env, e.Name)
					return true
				}
			}

			for _, rm := range recipe.RemoveWorkspaces {
				if strings.Contains(e.Value, "$(workspaces."+rm+".path)") {
					*env = append(*env, e.Name)
					return true
				}
			}

			return false
		}
	}

	templateEnv := make([]string, 0, 5)
	if task.Spec.StepTemplate != nil || recipe.PreferStepTemplate {
		if task.Spec.StepTemplate == nil {
			task.Spec.StepTemplate = &pipeline.StepTemplate{}
		}
		task.Spec.StepTemplate.VolumeMounts = slices.DeleteFunc(task.Spec.StepTemplate.VolumeMounts, func(vm core.VolumeMount) bool {
			for _, rm := range recipe.RemoveWorkspaces {
				if vm.Name == rm {
					return true
				}
			}

			for _, rm := range recipe.RemoveVolumes {
				if vm.Name == rm {
					return true
				}
			}

			return false
		})

		task.Spec.StepTemplate.VolumeMounts = append(task.Spec.StepTemplate.VolumeMounts, recipe.AddVolumeMount...)

		task.Spec.StepTemplate.Env = slices.DeleteFunc(task.Spec.StepTemplate.Env, removeEnv(&templateEnv))
	}

	for i := range task.Spec.Steps {
		env := make([]string, 0, 5)

		for j := range task.Spec.Steps[i].Env {
			task.Spec.Steps[i].Env[j].Value = applyReplacements(task.Spec.Steps[i].Env[j].Value, recipe.Replacements)
		}

		task.Spec.Steps[i].Env = slices.DeleteFunc(task.Spec.Steps[i].Env, removeEnv(&env))

		task.Spec.Steps[i].Env = append(task.Spec.Steps[i].Env, recipe.AddEnvironment...)

		if task.Spec.StepTemplate == nil {
			task.Spec.Steps[i].VolumeMounts = append(task.Spec.Steps[i].VolumeMounts, recipe.AddVolumeMount...)
		}

		task.Spec.Steps[i].VolumeMounts = slices.DeleteFunc(task.Spec.Steps[i].VolumeMounts, func(vm core.VolumeMount) bool {
			for _, rm := range recipe.RemoveVolumes {
				if vm.Name == rm {
					return true
				}
			}

			return false
		})

		if len(task.Spec.Steps[i].VolumeMounts) == 0 {
			task.Spec.Steps[i].VolumeMounts = nil
		}

		task.Spec.Steps[i].WorkingDir = applyReplacements(task.Spec.Steps[i].WorkingDir, recipe.Replacements)

		rx := map[*regexp.Regexp]string{}
		for old, new := range recipe.RegexReplacements {
			ex, err := regexp.Compile(old)
			if err != nil {
				panic(err)
			}
			rx[ex] = new
		}
		for ex, new := range rx {
			task.Spec.Steps[i].WorkingDir = ex.ReplaceAllString(task.Spec.Steps[i].WorkingDir, new)
		}

		if !isShell(task.Spec.Steps[i].Script) {
			continue
		}

		if len(recipe.Replacements) > 0 {
			task.Spec.Steps[i].Script = applyReplacements(task.Spec.Steps[i].Script, recipe.Replacements)
		}

		r := strings.NewReader(task.Spec.Steps[i].Script)
		f, err := parser.Parse(r, task.Spec.Steps[i].Name+"_script.sh")
		if err != nil {
			return err
		}

		for _, rm := range templateEnv {
			f.Stmts = removeEnvUse(f, rm)
		}
		for _, rm := range env {
			f.Stmts = removeEnvUse(f, rm)
		}
		if len(recipe.RegexReplacements) > 0 {
			f.Stmts = replaceLiterals(f, rx)
		}

		f.Stmts = removeUnusedFunctions(f)

		buf := bytes.Buffer{}
		if err := printer.Print(&buf, f); err != nil {
			return err
		}

		task.Spec.Steps[i].Script = buf.String()
	}

	if recipe.useSource || recipe.useCachi2 {
		args := []string{"use"}

		if recipe.useSource {
			args = append(args, fmt.Sprintf("$(params.SOURCE_ARTIFACT)=/var/workdir/%s", "source"))
		}

		if recipe.useCachi2 {
			args = append(args, fmt.Sprintf("$(params.CACHI2_ARTIFACT)=/var/workdir/%s", "cachi2"))
		}

		task.Spec.Steps = append([]pipeline.Step{{
			Name:  "use-trusted-artifact",
			Image: image,
			Args:  args,
		}}, task.Spec.Steps...)
	}
	if recipe.createSource || recipe.createCachi2 {
		args := []string{
			"create",
			"--store",
			"$(params.ociStorage)",
		}

		if recipe.createSource {
			args = append(args, "$(results.SOURCE_ARTIFACT.path)=/var/workdir/source")
		}

		if recipe.createCachi2 {
			args = append(args, "$(results.CACHI2_ARTIFACT.path)=/var/workdir/cachi2")
		}

		create := pipeline.Step{
			Name:  "create-trusted-artifact",
			Image: image,
			Env: []core.EnvVar{
				{
					Name:  "IMAGE_EXPIRES_AFTER",
					Value: "$(params.ociArtifactExpiresAfter)",
				},
			},
			Args: args,
		}

		if task.Spec.StepTemplate == nil && !recipe.PreferStepTemplate {
			create.VolumeMounts = []core.VolumeMount{workdirVolumeMount}
		}
		task.Spec.Steps = append(task.Spec.Steps, create)
	}

	if err := format(task); err != nil {
		return err
	}

	return nil
}