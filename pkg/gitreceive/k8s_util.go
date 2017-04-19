package gitreceive

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pborman/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/pkg/api/v1"

	"github.com/deis/builder/pkg/k8s"
)

const (
	slugBuilderName   = "deis-slugbuilder"
	dockerBuilderName = "deis-dockerbuilder"

	tarPath          = "TAR_PATH"
	putPath          = "PUT_PATH"
	cachePath        = "CACHE_PATH"
	debugKey         = "DEIS_DEBUG"
	sourceVersion    = "SOURCE_VERSION"
	objectStore      = "objectstorage-keyfile"
	dockerSocketName = "docker-socket"
	dockerSocketPath = "/var/run/docker.sock"
	builderStorage   = "BUILDER_STORAGE"
	objectStorePath  = "/var/run/secrets/deis/objectstore/creds"
	envRoot          = "/tmp/env"
)

func dockerBuilderPodName(appName, shortSha string) string {
	uid := uuid.New()[:8]
	// NOTE(bacongobbler): pod names cannot exceed 63 characters in length, so we truncate
	// the application name to stay under that limit when adding all the extra metadata to the name
	if len(appName) > 33 {
		appName = appName[:33]
	}
	return fmt.Sprintf("dockerbuild-%s-%s-%s", appName, shortSha, uid)
}

func slugBuilderPodName(appName, shortSha string) string {
	uid := uuid.New()[:8]
	// NOTE(bacongobbler): pod names cannot exceed 63 characters in length, so we truncate
	// the application name to stay under that limit when adding all the extra metadata to the name
	if len(appName) > 35 {
		appName = appName[:35]
	}
	return fmt.Sprintf("slugbuild-%s-%s-%s", appName, shortSha, uid)
}

func dockerBuilderPod(
	debug bool,
	name,
	namespace string,
	env map[string]interface{},
	tarKey,
	gitShortHash string,
	imageName,
	storageType,
	image,
	registryHost,
	registryPort string,
	registryEnv map[string]string,
	pullPolicy v1.PullPolicy,
	nodeSelector map[string]string,
) *v1.Pod {

	pod := buildPod(debug, name, namespace, pullPolicy, nodeSelector, env)

	// inject application envvars as a special envvar which will be handled by dockerbuilder to
	// inject them as build-time variables.
	// NOTE(bacongobbler): docker-py takes buildargs as a json string in the form of
	//
	// {"KEY": "value"}
	//
	// So we need to translate the map into json.
	if _, ok := env["DEIS_DOCKER_BUILD_ARGS_ENABLED"]; ok {
		dockerBuildArgs, _ := json.Marshal(env)
		addEnvToPod(pod, "DOCKER_BUILD_ARGS", string(dockerBuildArgs))
	}

	pod.Spec.Containers[0].Name = dockerBuilderName
	pod.Spec.Containers[0].Image = image

	addEnvToPod(pod, tarPath, tarKey)
	addEnvToPod(pod, sourceVersion, gitShortHash)
	addEnvToPod(pod, "IMG_NAME", imageName)
	addEnvToPod(pod, builderStorage, storageType)
	// inject existing DEIS_REGISTRY_SERVICE_HOST and PORT info to dockerbuilder
	// see https://github.com/deis/dockerbuilder/issues/83
	addEnvToPod(pod, "DEIS_REGISTRY_SERVICE_HOST", registryHost)
	addEnvToPod(pod, "DEIS_REGISTRY_SERVICE_PORT", registryPort)

	for key, value := range registryEnv {
		addEnvToPod(pod, key, value)
	}

	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
		Name:      dockerSocketName,
		MountPath: dockerSocketPath,
	})

	pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
		Name: dockerSocketName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: dockerSocketPath,
			},
		},
	})

	return &pod
}

func slugbuilderPod(
	debug bool,
	name,
	namespace string,
	envSecretName string,
	tarKey,
	putKey,
	cacheKey,
	gitShortHash string,
	buildpackURL,
	storageType,
	image string,
	pullPolicy v1.PullPolicy,
	nodeSelector map[string]string,
) *v1.Pod {

	pod := buildPod(debug, name, namespace, pullPolicy, nodeSelector, nil)

	pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
		Name: envSecretName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: envSecretName,
			},
		},
	})

	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
		Name:      envSecretName,
		MountPath: envRoot,
		ReadOnly:  true,
	})

	pod.Spec.Containers[0].Name = slugBuilderName
	pod.Spec.Containers[0].Image = image

	// If cacheKey is set, add it to environment
	if cacheKey != "" {
		addEnvToPod(pod, cachePath, cacheKey)
	}

	addEnvToPod(pod, tarPath, tarKey)
	addEnvToPod(pod, putPath, putKey)
	addEnvToPod(pod, sourceVersion, gitShortHash)
	addEnvToPod(pod, builderStorage, storageType)

	if buildpackURL != "" {
		addEnvToPod(pod, "BUILDPACK_URL", buildpackURL)
	}

	return &pod
}

func buildPod(
	debug bool,
	name,
	namespace string,
	pullPolicy v1.PullPolicy,
	nodeSelector map[string]string,
	env map[string]interface{}) v1.Pod {

	pod := v1.Pod{
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					ImagePullPolicy: pullPolicy,
				},
			},
			Volumes: []v1.Volume{},
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"heritage": name,
			},
		},
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
		Name: objectStore,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: objectStore,
			},
		},
	})

	pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
		{
			Name:      objectStore,
			MountPath: objectStorePath,
			ReadOnly:  true,
		},
	}

	if len(pod.Spec.Containers) > 0 {
		for k, v := range env {
			pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, v1.EnvVar{
				Name:  k,
				Value: fmt.Sprintf("%v", v),
			})
		}
	}

	if len(nodeSelector) > 0 {
		pod.Spec.NodeSelector = nodeSelector
	}

	if debug {
		addEnvToPod(pod, debugKey, "1")
	}

	return pod
}

func addEnvToPod(pod v1.Pod, key, value string) {
	if len(pod.Spec.Containers) > 0 {
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, v1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
}

// waitForPod waits for a pod in state running, succeeded or failed
func waitForPod(pw *k8s.PodWatcher, ns, podName string, ticker, interval, timeout time.Duration) error {
	condition := func(pod *v1.Pod) (bool, error) {
		if pod.Status.Phase == v1.PodRunning {
			return true, nil
		}
		if pod.Status.Phase == v1.PodSucceeded {
			return true, nil
		}
		if pod.Status.Phase == v1.PodFailed {
			return true, fmt.Errorf("Giving up; pod went into failed status: \n[%s]:%s", pod.Status.Reason, pod.Status.Message)
		}
		return false, nil
	}

	quit := progress("...", ticker)
	err := waitForPodCondition(pw, ns, podName, condition, interval, timeout)
	quit <- true
	<-quit
	return err
}

// waitForPodEnd waits for a pod in state succeeded or failed
func waitForPodEnd(pw *k8s.PodWatcher, ns, podName string, interval, timeout time.Duration) error {
	condition := func(pod *v1.Pod) (bool, error) {
		if pod.Status.Phase == v1.PodSucceeded {
			return true, nil
		}
		if pod.Status.Phase == v1.PodFailed {
			return true, nil
		}
		return false, nil
	}

	return waitForPodCondition(pw, ns, podName, condition, interval, timeout)
}

// waitForPodCondition waits for a pod in state defined by a condition (func)
func waitForPodCondition(pw *k8s.PodWatcher, ns, podName string, condition func(pod *v1.Pod) (bool, error),
	interval, timeout time.Duration) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		pods, err := pw.Store.List(labels.Set{"heritage": podName}.AsSelector())
		if err != nil || len(pods) == 0 {
			return false, nil
		}

		done, err := condition(pods[0])
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}

		return false, nil
	})
}

func progress(msg string, interval time.Duration) chan bool {
	tick := time.Tick(interval)
	quit := make(chan bool)
	go func() {
		for {
			select {
			case <-quit:
				close(quit)
				return
			case <-tick:
				fmt.Println(msg)
			}
		}
	}()
	return quit
}

func createAppEnvConfigSecret(secretsClient v1.SecretInterface, secretName string, env map[string]interface{}) error {
	newSecret := new(v1.Secret)
	newSecret.Name = secretName
	newSecret.Type = v1.SecretTypeOpaque
	newSecret.Data = make(map[string][]byte)
	for k, v := range env {
		newSecret.Data[k] = []byte(fmt.Sprintf("%v", v))
	}
	if _, err := secretsClient.Create(newSecret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if _, err = secretsClient.Update(newSecret); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	return nil
}
