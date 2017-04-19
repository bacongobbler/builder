package gitreceive

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/arschles/assert"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/client-go/pkg/api/v1"
)

func TestDockerBuilderPodName(t *testing.T) {
	name := dockerBuilderPodName("demo", "12345678")
	if !strings.HasPrefix(name, "dockerbuild-demo-12345678-") {
		t.Errorf("expected pod name dockerbuild-demo-12345678-*, got %s", name)
	}

	name = dockerBuilderPodName("this-name-has-more-than-24-characters-in-length", "12345678")
	if !strings.HasPrefix(name, "dockerbuild-this-name-has-more-than-24-charac-12345678-") {
		t.Errorf("expected pod name dockerbuild-this-name-has-more-than-24-charac-12345678-*, got %s", name)
	}
	if len(name) > 63 {
		t.Errorf("expected dockerbuilder pod name length to be <= 63 characters in length, got %d", len(name))
	}
}

func TestSlugBuilderPodName(t *testing.T) {
	name := slugBuilderPodName("demo", "12345678")
	if !strings.HasPrefix(name, "slugbuild-demo-12345678-") {
		t.Errorf("expected pod name slugbuild-demo-12345678-*, got %s", name)
	}

	name = slugBuilderPodName("this-name-has-more-than-24-characters-in-length", "12345678")
	if !strings.HasPrefix(name, "slugbuild-this-name-has-more-than-24-characte-12345678-") {
		t.Errorf("expected pod name slugbuild-this-name-has-more-than-24-characte-12345678-*, got %s", name)
	}
	if len(name) > 63 {
		t.Errorf("expected slugbuilder pod name length to be <= 63 characters in length, got %d", len(name))
	}
}

type slugBuildCase struct {
	debug                      bool
	name                       string
	namespace                  string
	envSecretName              string
	tarKey                     string
	putKey                     string
	cacheKey                   string
	gitShortHash               string
	buildPack                  string
	slugBuilderImage           string
	slugBuilderImagePullPolicy v1.PullPolicy
	storageType                string
	builderPodNodeSelector     map[string]string
}

type dockerBuildCase struct {
	debug                        bool
	name                         string
	namespace                    string
	env                          map[string]interface{}
	tarKey                       string
	gitShortHash                 string
	imgName                      string
	dockerBuilderImage           string
	dockerBuilderImagePullPolicy v1.PullPolicy
	storageType                  string
	builderPodNodeSelector       map[string]string
}

func TestBuildPod(t *testing.T) {
	emptyEnv := make(map[string]interface{})

	env := make(map[string]interface{})
	env["KEY"] = "VALUE"
	buildArgsEnv := make(map[string]interface{})
	buildArgsEnv["DEIS_DOCKER_BUILD_ARGS_ENABLED"] = "1"
	buildArgsEnv["KEY"] = "VALUE"
	envSecretName := "test-build-env"
	var pod *v1.Pod

	emptyNodeSelector := make(map[string]string)

	nodeSelector1 := make(map[string]string)
	nodeSelector1["disk"] = "ssd"

	nodeSelector2 := make(map[string]string)
	nodeSelector2["disk"] = "magnetic"
	nodeSelector2["network"] = "fast"

	slugBuilds := []slugBuildCase{
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", envSecretName, "tar", "put-url", "", "deadbeef", "", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "buildpack", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "buildpack", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "buildpack", "customimage", v1.PullAlways, "", nil},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "buildpack", "customimage", v1.PullIfNotPresent, "", nodeSelector1},
		{true, "test", "default", envSecretName, "tar", "put-url", "cache-url", "deadbeef", "buildpack", "customimage", v1.PullNever, "", nodeSelector2},
	}

	for _, build := range slugBuilds {
		pod = slugbuilderPod(
			build.debug,
			build.name,
			build.namespace,
			build.envSecretName,
			build.tarKey,
			build.putKey,
			build.cacheKey,
			build.gitShortHash,
			build.buildPack,
			build.storageType,
			build.slugBuilderImage,
			build.slugBuilderImagePullPolicy,
			build.builderPodNodeSelector,
		)

		if pod.ObjectMeta.Name != build.name {
			t.Errorf("expected %v but returned %v ", build.name, pod.ObjectMeta.Name)
		}

		if pod.ObjectMeta.Namespace != build.namespace {
			t.Errorf("expected %v but returned %v ", build.namespace, pod.ObjectMeta.Namespace)
		}

		checkForEnv(t, pod, "SOURCE_VERSION", build.gitShortHash)
		checkForEnv(t, pod, "TAR_PATH", build.tarKey)
		checkForEnv(t, pod, "PUT_PATH", build.putKey)

		if build.cacheKey == "" {
			if cachePath, err := envValueFromKey(pod, "CACHE_PATH"); err == nil {
				t.Errorf("expected CACHE_PATH not to be defined but it was defined with %v", cachePath)
			}
		} else {
			checkForEnv(t, pod, "CACHE_PATH", build.cacheKey)
		}

		if build.buildPack != "" {
			checkForEnv(t, pod, "BUILDPACK_URL", build.buildPack)
		}

		if build.slugBuilderImage != "" {
			if pod.Spec.Containers[0].Image != build.slugBuilderImage {
				t.Errorf("expected %v but returned %v ", build.slugBuilderImage, pod.Spec.Containers[0].Image)
			}
		}
		if build.slugBuilderImagePullPolicy != "" {
			if pod.Spec.Containers[0].ImagePullPolicy != build.slugBuilderImagePullPolicy {
				t.Errorf("expected %v but returned %v", build.slugBuilderImagePullPolicy, pod.Spec.Containers[0].ImagePullPolicy)
			}
		}

		if len(pod.Spec.NodeSelector) > 0 || len(build.builderPodNodeSelector) > 0 {
			assert.Equal(t, pod.Spec.NodeSelector, build.builderPodNodeSelector, "node selector")
		}
	}

	dockerBuilds := []dockerBuildCase{
		{true, "test", "default", emptyEnv, "tar", "deadbeef", "", "", v1.PullAlways, "", nodeSelector1},
		{true, "test", "default", env, "tar", "deadbeef", "", "", v1.PullAlways, "", nodeSelector2},
		{true, "test", "default", emptyEnv, "tar", "deadbeef", "img", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", env, "tar", "deadbeef", "img", "", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", env, "tar", "deadbeef", "img", "customimage", v1.PullAlways, "", emptyNodeSelector},
		{true, "test", "default", env, "tar", "deadbeef", "img", "customimage", v1.PullIfNotPresent, "", emptyNodeSelector},
		{true, "test", "default", env, "tar", "deadbeef", "img", "customimage", v1.PullNever, "", nil},
		{true, "test", "default", buildArgsEnv, "tar", "deadbeef", "img", "customimage", v1.PullIfNotPresent, "", emptyNodeSelector},
	}
	regEnv := map[string]string{"REG_LOC": "on-cluster"}
	for _, build := range dockerBuilds {
		pod = dockerBuilderPod(
			build.debug,
			build.name,
			build.namespace,
			build.env,
			build.tarKey,
			build.gitShortHash,
			build.imgName,
			build.storageType,
			build.dockerBuilderImage,
			"localhost",
			"5555",
			regEnv,
			build.dockerBuilderImagePullPolicy,
			build.builderPodNodeSelector,
		)

		if pod.ObjectMeta.Name != build.name {
			t.Errorf("expected %v but returned %v ", build.name, pod.ObjectMeta.Name)
		}
		if pod.ObjectMeta.Namespace != build.namespace {
			t.Errorf("expected %v but returned %v ", build.namespace, pod.ObjectMeta.Namespace)
		}

		checkForEnv(t, pod, "SOURCE_VERSION", build.gitShortHash)
		checkForEnv(t, pod, "TAR_PATH", build.tarKey)
		checkForEnv(t, pod, "IMG_NAME", build.imgName)
		checkForEnv(t, pod, "REG_LOC", "on-cluster")
		if _, ok := build.env["DEIS_DOCKER_BUILD_ARGS_ENABLED"]; ok {
			checkForEnv(t, pod, "DOCKER_BUILD_ARGS", `{"DEIS_DOCKER_BUILD_ARGS_ENABLED":"1","KEY":"VALUE"}`)
		}
		if build.dockerBuilderImage != "" {
			if pod.Spec.Containers[0].Image != build.dockerBuilderImage {
				t.Errorf("expected %v but returned %v", build.dockerBuilderImage, pod.Spec.Containers[0].Image)
			}
		}
		if build.dockerBuilderImagePullPolicy != "" {
			if pod.Spec.Containers[0].ImagePullPolicy != "" {
				if pod.Spec.Containers[0].ImagePullPolicy != build.dockerBuilderImagePullPolicy {
					t.Errorf("expected %v but returned %v", build.dockerBuilderImagePullPolicy, pod.Spec.Containers[0].ImagePullPolicy)
				}
			}
		}

		if len(pod.Spec.NodeSelector) > 0 || len(build.builderPodNodeSelector) > 0 {
			assert.Equal(t, pod.Spec.NodeSelector, build.builderPodNodeSelector, "node selector")
		}
	}
}

func checkForEnv(t *testing.T, pod *v1.Pod, key, expVal string) {
	val, err := envValueFromKey(pod, key)
	if err != nil {
		t.Errorf("%v", err)
	}
	if expVal != val {
		t.Errorf("expected %v but returned %v ", expVal, val)
	}
}

func envValueFromKey(pod *v1.Pod, key string) (string, error) {
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == key {
			return env.Value, nil
		}
	}

	return "", fmt.Errorf("no key with name %v found in pod env", key)
}

func TestCreateAppEnvConfigSecretErr(t *testing.T) {
	expectedErr := errors.New("get secret error")
	clientset := fake.NewSimpleClientset()
	err := createAppEnvConfigSecret(clientset.Secrets(""), "test", nil)
	assert.Err(t, err, expectedErr)
}

func TestCreateAppEnvConfigSecretSuccess(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	err := createAppEnvConfigSecret(clientset.Secrets(""), "test", nil)
	assert.NoErr(t, err)
}
