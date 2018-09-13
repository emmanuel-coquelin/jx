package prow

import (
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/jenkins-x/jx/pkg/kube"
	build "github.com/knative/build/pkg/apis/build/v1alpha1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/plugins"
	"github.com/jenkins-x/jx/pkg/util"
)

const (
	Hook                           = "hook"
	DefaultProwReleaseName         = "jx-prow"
	DefaultKnativeBuildReleaseName = "jx-knative-build"
	ProwVersion                    = "0.0.26"
	KnativeBuildVersion            = "0.0.6"
	ChartProw                      = "jenkins-x/prow"
	ChartKnativeBuild              = "jenkins-x/knative-build"

	Application Kind = "APPLICATION"
	Environment Kind = "ENVIRONMENT"
)

type Kind string

// Options for prow
type Options struct {
	KubeClient kubernetes.Interface
	Repos      []string
	NS         string
	Kind       Kind
}

func add(kubeClient kubernetes.Interface, repos []string, ns string, kind Kind) error {

	if len(repos) == 0 {
		return fmt.Errorf("no repo defined")
	}
	o := Options{
		KubeClient: kubeClient,
		Repos:      repos,
		NS:         ns,
		Kind:       kind,
	}

	err := o.AddProwConfig()
	if err != nil {
		return err
	}

	return o.AddProwPlugins()
}

func AddEnvironment(kubeClient kubernetes.Interface, repos []string, ns string) error {
	return add(kubeClient, repos, ns, Environment)
}

func AddApplication(kubeClient kubernetes.Interface, repos []string, ns string) error {
	return add(kubeClient, repos, ns, Application)
}

// create git repo?
// get config and update / overwrite repos?
// should we get the existing CM and do a diff?
// should we just be using git for config and use prow to auto update via gitops?

func (o *Options) createPreSubmitEnvironment() config.Presubmit {
	ps := config.Presubmit{}

	ps.Name = "promotion-gate"
	ps.AlwaysRun = true
	ps.SkipReport = false
	ps.Context = "promotion-gate"
	ps.Agent = "knative-build"

	spec := &build.BuildSpec{
		Steps: []v1.Container{
			{
				Image: "jenkinsxio/builder-base:latest",
				Args:  []string{"jx", "step", "helm", "build"},
				Env: []v1.EnvVar{
					{Name: "DEPLOY_NAMESPACE", Value: "jx-staging"},
					{Name: "CHART_REPOSITORY", Value: "http://jenkins-x-chartmuseum:8080"},
					{Name: "XDG_CONFIG_HOME", Value: "/home/jenkins"},
					{Name: "GIT_COMMITTER_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_NAME", Value: "jenkins-x-bot"},
					{Name: "GIT_COMMITTER_NAME", Value: "jenkins-x-bot"},
				},
			},
		},
		ServiceAccountName: "jenkins",
	}

	ps.BuildSpec = spec
	ps.RerunCommand = "/test this"
	ps.Trigger = "(?m)^/test( all| this),?(\\s+|$)"

	return ps
}

func (o *Options) createPostSubmitEnvironment() config.Postsubmit {
	ps := config.Postsubmit{}
	ps.Name = "promotion"
	ps.Agent = "knative-build"
	ps.Branches = []string{"master"}

	spec := &build.BuildSpec{
		Steps: []v1.Container{
			{
				Image: "jenkinsxio/builder-base:latest",
				Args:  []string{"jx", "step", "helm", "apply"},
				Env: []v1.EnvVar{
					{Name: "DEPLOY_NAMESPACE", Value: "jx-staging"},
					{Name: "CHART_REPOSITORY", Value: "http://jenkins-x-chartmuseum:8080"},
					{Name: "XDG_CONFIG_HOME", Value: "/home/jenkins"},
					{Name: "GIT_COMMITTER_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_NAME", Value: "jenkins-x-bot"},
					{Name: "GIT_COMMITTER_NAME", Value: "jenkins-x-bot"},
				},
			},
		},
		ServiceAccountName: "jenkins",
	}
	ps.BuildSpec = spec
	return ps
}

func (o *Options) createPostSubmitMavenApplication() config.Postsubmit {
	ps := config.Postsubmit{}
	ps.Branches = []string{"master"}
	ps.Name = "release"
	ps.Agent = "knative-build"

	spec := &build.BuildSpec{
		Steps: []v1.Container{
			{
				Image: "jenkinsxio/jenkins-maven:latest",
				Env: []v1.EnvVar{
					{Name: "GIT_COMMITTER_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_EMAIL", Value: "jenkins-x@googlegroups.com"},
					{Name: "GIT_AUTHOR_NAME", Value: "jenkins-x-bot"},
					{Name: "GIT_COMMITTER_NAME", Value: "jenkins-x-bot"},
					{Name: "XDG_CONFIG_HOME", Value: "/home/jenkins"},
					{Name: "DOCKER_CONFIG", Value: "/home/jenkins/.docker/"},
					{Name: "DOCKER_REGISTRY", ValueFrom: &v1.EnvVarSource{

						ConfigMapKeyRef: &v1.ConfigMapKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "jenkins-x-docker-registry",
							},
							Key: "docker.registry",
						},
					}},
				},
				VolumeMounts: []v1.VolumeMount{
					v1.VolumeMount{Name: "jenkins-docker-cfg", MountPath: "/home/jenkins/.docker"},
					v1.VolumeMount{Name: "docker-sock-volume", MountPath: "/var/run/docker.sock"},
					v1.VolumeMount{Name: "jenkins-maven-settings", MountPath: "/root/.m2/"},
					v1.VolumeMount{Name: "jenkins-release-gpg", MountPath: "/home/jenkins/.gnupg"},
				},
			},
		},
		ServiceAccountName: "jenkins",
		Volumes: []v1.Volume{
			v1.Volume{Name: "jenkins-docker-cfg", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-docker-cfg"}}},
			v1.Volume{Name: "docker-sock-volume", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/run/docker.sock"}}},
			v1.Volume{Name: "jenkins-maven-settings", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-maven-settings"}}},
			v1.Volume{Name: "jenkins-release-gpg", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-release-gpg"}}},
		},
	}

	ps.BuildSpec = spec
	return ps
}

func (o *Options) createPreSubmitMavenApplication() config.Presubmit {
	ps := config.Presubmit{}

	ps.Context = "jenkins-engine-ci"
	ps.Name = "jenkins-engine-ci"
	ps.RerunCommand = "/test this"
	ps.Trigger = "(?m)^/test( all| this),?(\\s+|$)"
	ps.AlwaysRun = false
	ps.SkipReport = false
	ps.Agent = "knative-build"

	spec := &build.BuildSpec{
		Steps: []v1.Container{
			{
				Image: "jenkinsxio/jenkins-maven:latest",
				Env: []v1.EnvVar{
					{Name: "DOCKER_CONFIG", Value: "/home/jenkins/.docker/"},
					{Name: "DOCKER_REGISTRY", ValueFrom: &v1.EnvVarSource{

						ConfigMapKeyRef: &v1.ConfigMapKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "jenkins-x-docker-registry",
							},
							Key: "docker.registry",
						},
					}},
				},
				VolumeMounts: []v1.VolumeMount{
					v1.VolumeMount{Name: "jenkins-docker-cfg", MountPath: "/home/jenkins/.docker"},
					v1.VolumeMount{Name: "docker-sock-volume", MountPath: "/var/run/docker.sock"},
					v1.VolumeMount{Name: "jenkins-maven-settings", MountPath: "/root/.m2/"},
					v1.VolumeMount{Name: "jenkins-release-gpg", MountPath: "/home/jenkins/.gnupg"},
				},
			},
		},
		ServiceAccountName: "jenkins",
		Volumes: []v1.Volume{
			v1.Volume{Name: "jenkins-docker-cfg", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-docker-cfg"}}},
			v1.Volume{Name: "docker-sock-volume", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/run/docker.sock"}}},
			v1.Volume{Name: "jenkins-maven-settings", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-maven-settings"}}},
			v1.Volume{Name: "jenkins-release-gpg", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "jenkins-release-gpg"}}},
		},
	}

	ps.BuildSpec = spec
	ps.RerunCommand = "/test this"
	ps.Trigger = "(?m)^/test( all| this),?(\\s+|$)"

	return ps
}

func (o *Options) createTide(existingRepos []string) config.Tide {
	// todo get the real URL, though we need to handle the multi cluster usecase where dev namespace may be another cluster, so pass it in as an arg?
	t := config.Tide{
		TargetURL: "https://tide.foo.bar",
	}

	var qs []config.TideQuery

	for _, r := range o.Repos {
		repos := existingRepos
		if !util.Contains(repos, r) {
			repos = append(repos, r)
		}

		q := config.TideQuery{
			Repos:         repos,
			Labels:        []string{"approved"},
			MissingLabels: []string{"do-not-merge", "do-not-merge/hold", "do-not-merge/work-in-progress", "needs-ok-to-test", "needs-rebase"},
		}
		qs = append(qs, q)
	}

	queries := qs

	t.Queries = queries

	// todo JR not sure if we need the contexts if we add the branch protection plugin
	//orgPolicies := make(map[string]config.TideOrgContextPolicy)
	//repoPolicies := make(map[string]config.TideRepoContextPolicy)
	//
	//ctxPolicy := config.TideContextPolicy{}
	//
	//repoPolicy := config.TideRepoContextPolicy{}
	//
	//repoPolicies[""] = repoPolicy
	//orgPolicy := config.TideOrgContextPolicy{
	//	TideContextPolicy: ctxPolicy,
	//	Repos:             repoPolicies,
	//}
	//
	//orgPolicies[""] = orgPolicy

	myTrue := true
	t.ContextOptions = config.TideContextPolicyOptions{
		TideContextPolicy: config.TideContextPolicy{
			FromBranchProtection: &myTrue,
			SkipUnknownContexts:  &myTrue,
		},
		//Orgs: orgPolicies,
	}

	return t
}

// AddProwConfig adds config to prow
func (o *Options) AddProwConfig() error {
	var preSubmit config.Presubmit
	var postSubmit config.Postsubmit

	switch o.Kind {
	case Application:
		preSubmit = o.createPreSubmitMavenApplication()
		postSubmit = o.createPostSubmitMavenApplication()
	case Environment:
		preSubmit = o.createPreSubmitEnvironment()
		postSubmit = o.createPostSubmitEnvironment()
	default:
		return fmt.Errorf("unknown prow config kind %s", o.Kind)
	}

	cm, err := o.KubeClient.CoreV1().ConfigMaps(o.NS).Get("config", metav1.GetOptions{})
	create := true
	prowConfig := &config.Config{}
	// config doesn't exist, creating
	if err != nil {
		prowConfig.Presubmits = make(map[string][]config.Presubmit)
		prowConfig.Postsubmits = make(map[string][]config.Postsubmit)
		prowConfig.Tide = o.createTide([]string{})
	} else {
		// config exists, updating
		create = false
		err = yaml.Unmarshal([]byte(cm.Data["config.yaml"]), &prowConfig)
		if err != nil {
			return err
		}
		if len(prowConfig.Presubmits) == 0 {
			prowConfig.Presubmits = make(map[string][]config.Presubmit)
		}
		if len(prowConfig.Postsubmits) == 0 {
			prowConfig.Postsubmits = make(map[string][]config.Postsubmit)
		}

		if len(prowConfig.Tide.Queries) > 0 {
			repos := prowConfig.Tide.Queries[0].Repos
			prowConfig.Tide = o.createTide(repos)
		} else {
			prowConfig.Tide = o.createTide([]string{})
		}
	}

	for _, r := range o.Repos {
		prowConfig.Presubmits[r] = []config.Presubmit{preSubmit}
		prowConfig.Postsubmits[r] = []config.Postsubmit{postSubmit}
	}

	configYAML, err := yaml.Marshal(prowConfig)
	if err != nil {
		return err
	}

	data := make(map[string]string)
	data["config.yaml"] = string(configYAML)
	cm = &v1.ConfigMap{
		Data: data,
		ObjectMeta: metav1.ObjectMeta{
			Name: "config",
		},
	}

	if create {
		_, err = o.KubeClient.CoreV1().ConfigMaps(o.NS).Create(cm)
	} else {
		_, err = o.KubeClient.CoreV1().ConfigMaps(o.NS).Update(cm)
	}

	return err

}

// AddProwPlugins adds plugins to prow
func (o *Options) AddProwPlugins() error {

	pluginsList := []string{"config-updater", "approve", "assign", "blunderbuss", "help", "hold", "lgtm", "lifecycle", "size", "trigger", "wip", "heart"}

	cm, err := o.KubeClient.CoreV1().ConfigMaps(o.NS).Get("plugins", metav1.GetOptions{})
	create := true
	pluginConfig := &plugins.Configuration{}
	if err != nil {
		pluginConfig.Plugins = make(map[string][]string)
		pluginConfig.Approve = []plugins.Approve{}

		pluginConfig.ConfigUpdater.Maps = make(map[string]plugins.ConfigMapSpec)
		pluginConfig.ConfigUpdater.Maps["prow/config.yaml"] = plugins.ConfigMapSpec{Name: "config"}
		pluginConfig.ConfigUpdater.Maps["prow/plugins.yaml"] = plugins.ConfigMapSpec{Name: "plugins"}

	} else {
		create = false
		err = yaml.Unmarshal([]byte(cm.Data["plugins.yaml"]), &pluginConfig)
		if err != nil {
			return err
		}
		if pluginConfig == nil {
			pluginConfig = &plugins.Configuration{}
		}
		if len(pluginConfig.Plugins) == 0 {
			pluginConfig.Plugins = make(map[string][]string)
		}
		if len(pluginConfig.Approve) == 0 {
			pluginConfig.Approve = []plugins.Approve{}
		}
	}

	for _, r := range o.Repos {
		pluginConfig.Plugins[r] = pluginsList

		a := plugins.Approve{
			Repos:               []string{r},
			ReviewActsAsApprove: true,
			LgtmActsAsApprove:   true,
		}
		pluginConfig.Approve = append(pluginConfig.Approve, a)

	}

	pluginYAML, err := yaml.Marshal(pluginConfig)
	if err != nil {
		return err
	}

	data := make(map[string]string)
	data["plugins.yaml"] = string(pluginYAML)
	cm = &v1.ConfigMap{
		Data: data,
		ObjectMeta: metav1.ObjectMeta{
			Name: "plugins",
		},
	}
	if create {
		_, err = o.KubeClient.CoreV1().ConfigMaps(o.NS).Create(cm)
	} else {
		_, err = o.KubeClient.CoreV1().ConfigMaps(o.NS).Update(cm)
	}

	return err
}

func IsProwInstalled(kubeClient kubernetes.Interface, ns string) (bool, error) {

	podCount, err := kube.DeploymentPodCount(kubeClient, Hook, ns)
	if err != nil {
		return false, fmt.Errorf("failed when looking for hook deployment: %v", err)
	}
	if podCount == 0 {
		return false, nil
	}
	return true, nil
}
