package scan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/debricked/cli/internal/ci"
	"github.com/debricked/cli/internal/ci/env"
	"github.com/debricked/cli/internal/client"
	"github.com/debricked/cli/internal/file"
	"github.com/debricked/cli/internal/git"
	"github.com/debricked/cli/internal/resolution"
	"github.com/debricked/cli/internal/tui"
	"github.com/debricked/cli/internal/upload"
	"github.com/fatih/color"
)

var (
	BadOptsErr      = errors.New("failed to type case IOptions")
	FailPipelineErr = errors.New("")
)

type IScanner interface {
	Scan(o IOptions) error
}

type IOptions interface{}

type DebrickedScanner struct {
	client    *client.IDebClient
	finder    file.IFinder
	uploader  *upload.IUploader
	ciService ci.IService
	resolver  resolution.IResolver
}

type DebrickedOptions struct {
	Path            string
	Resolve         bool
	Exclusions      []string
	RepositoryName  string
	CommitName      string
	BranchName      string
	CommitAuthor    string
	RepositoryUrl   string
	IntegrationName string
	PassOnTimeOut   bool
}

func NewDebrickedScanner(
	c *client.IDebClient,
	finder file.IFinder,
	uploader upload.IUploader,
	ciService ci.IService,
	resolver resolution.IResolver,
) *DebrickedScanner {
	return &DebrickedScanner{
		c,
		finder,
		&uploader,
		ciService,
		resolver,
	}
}

func (dScanner *DebrickedScanner) Scan(o IOptions) error {
	dOptions, ok := o.(DebrickedOptions)
	if !ok {
		return BadOptsErr
	}

	e, _ := dScanner.ciService.Find()

	MapEnvToOptions(&dOptions, e)

	if err := SetWorkingDirectory(&dOptions); err != nil {
		return err
	}

	gitMetaObject, err := git.NewMetaObject(
		dOptions.Path,
		dOptions.RepositoryName,
		dOptions.CommitName,
		dOptions.BranchName,
		dOptions.CommitAuthor,
		dOptions.RepositoryUrl,
	)
	if err != nil {
		return err
	}

	result, err := dScanner.scan(dOptions, *gitMetaObject)
	if err != nil {
		return dScanner.handleScanError(err, dOptions.PassOnTimeOut)
	}

	if result == nil {
		fmt.Println("Progress polling terminated due to long scan times. Please try again later")

		return nil
	}

	fmt.Printf("\n%d vulnerabilities found\n", result.VulnerabilitiesFound)
	fmt.Println("")
	failPipeline := false
	for _, rule := range result.AutomationRules {
		tui.NewRuleCard(os.Stdout, rule).Render()
		failPipeline = failPipeline || (rule.Triggered && rule.FailPipeline())
	}
	fmt.Printf("For full details, visit: %s\n\n", color.BlueString(result.DetailsUrl))
	if failPipeline {
		return FailPipelineErr
	}

	return nil
}

func (dScanner *DebrickedScanner) scan(options DebrickedOptions, gitMetaObject git.MetaObject) (*upload.UploadResult, error) {
	if options.Resolve {
		_, resErr := dScanner.resolver.Resolve([]string{options.Path}, options.Exclusions)
		if resErr != nil {
			return nil, resErr
		}
	}

	fileGroups, err := dScanner.finder.GetGroups(options.Path, options.Exclusions, false, file.StrictAll)
	if err != nil {
		return nil, err
	}

	uploaderOptions := upload.DebrickedOptions{FileGroups: fileGroups, GitMetaObject: gitMetaObject, IntegrationsName: options.IntegrationName}
	result, err := (*dScanner.uploader).Upload(uploaderOptions)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (dScanner *DebrickedScanner) handleScanError(err error, passOnTimeOut bool) error {
	if err == client.NoResErr && passOnTimeOut {
		fmt.Println(err)

		return nil
	}

	return err
}

// SetWorkingDirectory sets working directory in accordance with the path option
func SetWorkingDirectory(d *DebrickedOptions) error {
	absPath, _ := filepath.Abs(d.Path)
	err := os.Chdir(absPath)
	if err != nil {
		return err
	}
	d.Path = ""
	fmt.Printf("Working directory: %s\n", absPath)

	return nil
}

func MapEnvToOptions(o *DebrickedOptions, env env.Env) {
	if len(o.RepositoryName) == 0 {
		o.RepositoryName = env.Repository
	}
	if len(o.CommitName) == 0 {
		o.CommitName = env.Commit
	}
	if len(o.BranchName) == 0 {
		o.BranchName = env.Branch
	}
	if len(o.CommitAuthor) == 0 {
		o.CommitAuthor = env.Author
	}
	if len(o.RepositoryUrl) == 0 {
		o.RepositoryUrl = env.RepositoryUrl
	}
	if o.IntegrationName == "CLI" {
		if len(env.Integration) != 0 {
			o.IntegrationName = env.Integration
		}
	}
	if len(o.Path) == 0 && len(env.Filepath) > 0 {
		o.Path = env.Filepath
	}
}
