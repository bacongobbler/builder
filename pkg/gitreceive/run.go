package gitreceive

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/deis/pkg/log"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	builderconf "github.com/deis/builder/pkg/conf"
	"github.com/deis/builder/pkg/sys"
)

func readLine(line string) (string, string, string, error) {
	spl := strings.Split(line, " ")
	if len(spl) != 3 {
		return "", "", "", fmt.Errorf("malformed line [%s]", line)
	}
	return spl[0], spl[1], spl[2], nil
}

// Run runs the git-receive hook. This func is effectively the main for the git-receive hook,
// although it is called from the main in boot.go.
func Run(conf *Config, fs sys.FS, env sys.Env, storageDriver storagedriver.StorageDriver) error {
	log.Debug("Running git hook")

	builderKey, err := builderconf.GetBuilderKey()
	if err != nil {
		return err
	}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("Error getting kubernetes in-cluster config (%s)", err)
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("Error getting kubernetes client (%s)", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		oldRev, newRev, refName, err := readLine(line)

		if err != nil {
			return fmt.Errorf("reading STDIN (%s)", err)
		}

		log.Debug("read [%s,%s,%s]", oldRev, newRev, refName)

		// if we're processing a receive-pack on an existing repo, run a build
		if strings.HasPrefix(conf.SSHOriginalCommand, "git-receive-pack") {
			if err := build(conf, storageDriver, clientset, fs, env, builderKey, newRev); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}
