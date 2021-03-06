package cmd_test_helpers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/jenkins-x/jx/pkg/environments"

	resources_test "github.com/jenkins-x/jx/pkg/kube/resources/mocks"

	jenkinsv1 "github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/helm"
	helm_test "github.com/jenkins-x/jx/pkg/helm/mocks"
	"github.com/jenkins-x/jx/pkg/io/secrets"
	"github.com/jenkins-x/jx/pkg/jx/cmd"
	cmd_test "github.com/jenkins-x/jx/pkg/jx/cmd/mocks"
	"github.com/jenkins-x/jx/pkg/kube"
	vault_test "github.com/jenkins-x/jx/pkg/vault/mocks"
	"github.com/petergtz/pegomock"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
)

// Helpers for various app tests

// AppTestOptions contains all useful data from the test environment initialized by `prepareInitialPromotionEnv`
type AppTestOptions struct {
	ConfigureGitFn  environments.ConfigureGitFn
	CommonOptions   *cmd.CommonOptions
	FakeGitProvider *gits.FakeProvider
	DevRepo         *gits.FakeRepository
	DevEnvRepo      *gits.FakeRepository
	OrgName         string
	DevEnvRepoInfo  *gits.GitRepository
	DevEnv          *jenkinsv1.Environment
	MockHelmer      *helm_test.MockHelmer
	MockFactory     *cmd_test.MockFactory
	MockVaultClient *vault_test.MockClient
}

// AddApp modifies the environment git repo directly to add a dummy app
func (o *AppTestOptions) AddApp() (name string, alias string, version string, err error) {
	envDir, err := o.CommonOptions.EnvironmentsDir()
	if err != nil {
		return "", "", "", err
	}
	devEnvDir := filepath.Join(envDir, o.OrgName, o.DevEnvRepoInfo.Name)
	err = os.MkdirAll(devEnvDir, 0700)
	if err != nil {
		return "", "", "", err
	}
	fileName := filepath.Join(devEnvDir, helm.RequirementsFileName)
	requirements := helm.Requirements{}
	if _, err := os.Stat(fileName); err == nil {
		data, err := ioutil.ReadFile(fileName)
		if err != nil {
			return "", "", "", err
		}

		err = yaml.Unmarshal(data, &requirements)
		if err != nil {
			return "", "", "", err
		}
	}
	name = uuid.NewV4().String()
	alias = fmt.Sprintf("%s-alias", name)
	version = "0.0.1"
	requirements.Dependencies = append(requirements.Dependencies, &helm.Dependency{
		Name:       name,
		Alias:      alias,
		Version:    version,
		Repository: "http://fake.chartmuseum",
	})
	data, err := yaml.Marshal(requirements)
	if err != nil {
		return "", "", "", err
	}
	err = ioutil.WriteFile(fileName, data, 0755)
	if err != nil {
		return "", "", "", err
	}
	return name, alias, version, nil
}

// Cleanup must be run in a defer statement whenever CreateAppTestOptions is run
func (o *AppTestOptions) Cleanup() error {
	err := cmd.CleanupTestEnvironmentDir(o.CommonOptions)
	if err != nil {
		return err
	}
	return nil
}

// CreateAppTestOptions configures the mock environment for running apps related tests
func CreateAppTestOptions(gitOps bool, t *testing.T) *AppTestOptions {
	mockFactory := cmd_test.NewMockFactory()
	o := AppTestOptions{
		CommonOptions: &cmd.CommonOptions{
			Factory: mockFactory,
		},
	}
	testOrgName := uuid.NewV4().String()
	testRepoName := uuid.NewV4().String()
	devEnvRepoName := fmt.Sprintf("environment-%s-%s-dev", testOrgName, testRepoName)
	fakeRepo := gits.NewFakeRepository(testOrgName, testRepoName)
	devEnvRepo := gits.NewFakeRepository(testOrgName, devEnvRepoName)

	fakeGitProvider := gits.NewFakeProvider(fakeRepo, devEnvRepo)

	var devEnv *jenkinsv1.Environment
	if gitOps {
		devEnv = kube.NewPermanentEnvironmentWithGit("dev", fmt.Sprintf("https://fake.git/%s/%s.git", testOrgName,
			devEnvRepoName))
		devEnv.Spec.Source.URL = devEnvRepo.GitRepo.CloneURL
		devEnv.Spec.Source.Ref = "master"
		o.MockVaultClient = vault_test.NewMockClient()
		pegomock.When(mockFactory.SecretsLocation()).ThenReturn(pegomock.ReturnValue(secrets.VaultLocationKind))
		pegomock.When(mockFactory.CreateSystemVaultClient(pegomock.AnyString())).ThenReturn(pegomock.ReturnValue(o.
			MockVaultClient), pegomock.ReturnValue(nil))
	} else {
		devEnv = kube.NewPermanentEnvironment("dev")
	}
	o.MockHelmer = helm_test.NewMockHelmer()
	installerMock := resources_test.NewMockInstaller()
	cmd.ConfigureTestOptionsWithResources(o.CommonOptions,
		[]runtime.Object{},
		[]runtime.Object{
			devEnv,
		},
		gits.NewGitLocal(),
		fakeGitProvider,
		o.MockHelmer,
		installerMock,
	)

	err := cmd.CreateTestEnvironmentDir(o.CommonOptions)
	assert.NoError(t, err)
	o.ConfigureGitFn = func(dir string, gitInfo *gits.GitRepository, gitter gits.Gitter) error {
		err := gitter.Init(dir)
		if err != nil {
			return err
		}
		// Really we should have a dummy environment chart but for now let's just mock it out as needed
		err = os.MkdirAll(filepath.Join(dir, "templates"), 0700)
		if err != nil {
			return err
		}
		data, err := json.Marshal(devEnv)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(filepath.Join(dir, "templates", "dev-env.yaml"), data, 0755)
		if err != nil {
			return err
		}
		return gitter.AddCommit(dir, "Initial Commit")
	}
	o.FakeGitProvider = fakeGitProvider
	o.DevRepo = fakeRepo
	o.DevEnvRepo = devEnvRepo
	o.OrgName = testOrgName
	o.DevEnv = devEnv
	o.DevEnvRepoInfo = &gits.GitRepository{
		Name: devEnvRepoName,
	}
	return &o

}
