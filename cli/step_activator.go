package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bitrise-io/bitrise/log"
	"github.com/bitrise-io/bitrise/models"
	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/pointers"
	stepmanModels "github.com/bitrise-io/stepman/models"
)

type stepActivator struct {
}

func newStepActivator() stepActivator {
	return stepActivator{}
}

func (a stepActivator) activateStep(
	stepIDData models.StepIDData,
	buildRunResults *models.BuildRunResultsModel,
	stepDir string,
	workDir string,
	workflowStep *stepmanModels.StepModel,
	stepInfoPtr *stepmanModels.StepInfoModel,
) (stepYMLPth string, origStepYMLPth string, err error) {
	stepYMLPth = filepath.Join(workDir, "current_step.yml")

	if stepIDData.SteplibSource == "path" {
		log.Debugf("[BITRISE_CLI] - Local step found: (path:%s)", stepIDData.IDorURI)
		stepAbsLocalPth, err := pathutil.AbsPath(stepIDData.IDorURI)
		if err != nil {
			return "", "", err
		}

		exist, err := pathutil.IsDirExists(stepAbsLocalPth)
		if err != nil {
			return "", "", fmt.Errorf("failed to activate local step: failed to check if a directory exists at %s: %w", stepAbsLocalPth, err)
		} else if !exist {
			return "", "", fmt.Errorf("failed to activate local step: the provided directory doesn't exist: %s", stepAbsLocalPth)
		}

		log.Debug("stepAbsLocalPth:", stepAbsLocalPth, "|stepDir:", stepDir)

		origStepYMLPth = filepath.Join(stepAbsLocalPth, "step.yml")
		exist, err = pathutil.IsPathExists(origStepYMLPth)
		if err != nil {
			return "", "", fmt.Errorf("failed to activate local step: failed to check if step.yml exists at %s: %w", origStepYMLPth, err)
		} else if !exist {
			return "", "", fmt.Errorf("failed to activate local step: step.yml doesn't exist at %s", origStepYMLPth)
		}

		if err := command.CopyFile(origStepYMLPth, stepYMLPth); err != nil {
			return "", "", err
		}

		if err := command.CopyDir(stepAbsLocalPth, stepDir, true); err != nil {
			return "", "", err
		}
	} else if stepIDData.SteplibSource == "git" {
		log.Debugf("[BITRISE_CLI] - Remote step, with direct git uri: (uri:%s) (tag-or-branch:%s)", stepIDData.IDorURI, stepIDData.Version)
		repo, err := git.New(stepDir)
		if err != nil {
			return "", "", err
		}
		var cloneCmd *command.Model
		if stepIDData.Version == "" {
			cloneCmd = repo.Clone(stepIDData.IDorURI, "--depth=1")
		} else {
			cloneCmd = repo.CloneTagOrBranch(stepIDData.IDorURI, stepIDData.Version, "--depth=1")
		}
		if out, err := cloneCmd.RunAndReturnTrimmedCombinedOutput(); err != nil {
			if strings.HasPrefix(stepIDData.IDorURI, "git@") {
				log.Warnf(`Note: if the step's repository is an open source one,
you should probably use a "https://..." git clone URL,
instead of the "git@..." git clone URL which usually requires authentication
even if the repository is open source!`)
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return "", "", fmt.Errorf("command failed with exit status %d (%s): %w", exitErr.ExitCode(), cloneCmd.PrintableCommandArgs(), errors.New(out))
			}
			return "", "", err
		}

		if err := command.CopyFile(filepath.Join(stepDir, "step.yml"), stepYMLPth); err != nil {
			return "", "", err
		}
	} else if stepIDData.SteplibSource == "_" {
		log.Debugf("[BITRISE_CLI] - Steplib independent step, with direct git uri: (uri:%s) (tag-or-branch:%s)", stepIDData.IDorURI, stepIDData.Version)

		// Steplib independent steps are completly defined in workflow
		stepYMLPth = ""
		if err := workflowStep.FillMissingDefaults(); err != nil {
			return "", "", err
		}

		repo, err := git.New(stepDir)
		if err != nil {
			return "", "", err
		}
		if err := repo.CloneTagOrBranch(stepIDData.IDorURI, stepIDData.Version).Run(); err != nil {
			return "", "", err
		}
	} else if stepIDData.SteplibSource != "" {
		isUpdated := buildRunResults.IsStepLibUpdated(stepIDData.SteplibSource)
		stepInfo, didUpdate, err := activateStepLibStep(stepIDData, stepDir, stepYMLPth, isUpdated)
		if didUpdate {
			buildRunResults.StepmanUpdates[stepIDData.SteplibSource]++
		}

		stepInfoPtr.ID = stepInfo.ID
		if stepInfoPtr.Step.Title == nil || *stepInfoPtr.Step.Title == "" {
			stepInfoPtr.Step.Title = pointers.NewStringPtr(stepInfo.ID)
		}
		stepInfoPtr.Version = stepInfo.Version
		stepInfoPtr.LatestVersion = stepInfo.LatestVersion
		stepInfoPtr.OriginalVersion = stepInfo.OriginalVersion
		stepInfoPtr.GroupInfo = stepInfo.GroupInfo

		if err != nil {
			return "", "", err
		}
	} else {
		return "", "", fmt.Errorf("invalid stepIDData: no SteplibSource or LocalPath defined (%v)", stepIDData)
	}

	return stepYMLPth, origStepYMLPth, nil
}
