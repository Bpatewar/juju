// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package manual

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/utils"
	"github.com/juju/utils/ssh"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/manual"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/juju/names"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/network"
	"github.com/juju/juju/provider/common"
	"github.com/juju/juju/worker/terminationworker"
)

const (
	// BootstrapInstanceId is the instance ID used
	// for the manual provider's bootstrap instance.
	BootstrapInstanceId instance.Id = "manual:"
)

var (
	logger                                       = loggo.GetLogger("juju.provider.manual")
	manualCheckProvisioned                       = manual.CheckProvisioned
	manualDetectSeriesAndHardwareCharacteristics = manual.DetectSeriesAndHardwareCharacteristics
)

type manualEnviron struct {
	cfg                 *environConfig
	cfgmutex            sync.Mutex
	ubuntuUserInited    bool
	ubuntuUserInitMutex sync.Mutex
}

var errNoStartInstance = errors.New("manual provider cannot start instances")
var errNoStopInstance = errors.New("manual provider cannot stop instances")

// MaintainInstance is specified in the InstanceBroker interface.
func (*manualEnviron) MaintainInstance(args environs.StartInstanceParams) error {
	return nil
}

func (*manualEnviron) StartInstance(args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	return nil, errNoStartInstance
}

func (*manualEnviron) StopInstances(...instance.Id) error {
	return errNoStopInstance
}

func (e *manualEnviron) AllInstances() ([]instance.Instance, error) {
	return e.Instances([]instance.Id{BootstrapInstanceId})
}

func (e *manualEnviron) envConfig() (cfg *environConfig) {
	e.cfgmutex.Lock()
	cfg = e.cfg
	e.cfgmutex.Unlock()
	return cfg
}

func (e *manualEnviron) Config() *config.Config {
	return e.envConfig().Config
}

// PrepareForBootstrap is specified in the Environ interface.
func (e *manualEnviron) PrepareForBootstrap(ctx environs.BootstrapContext) error {
	if err := ensureBootstrapUbuntuUser(ctx, e.envConfig()); err != nil {
		return err
	}
	return nil
}

// Bootstrap is specified on the Environ interface.
func (e *manualEnviron) Bootstrap(ctx environs.BootstrapContext, args environs.BootstrapParams) (*environs.BootstrapResult, error) {
	envConfig := e.envConfig()
	host := envConfig.bootstrapHost()
	provisioned, err := manualCheckProvisioned(host)
	if err != nil {
		return nil, errors.Annotate(err, "failed to check provisioned status")
	}
	if provisioned {
		return nil, manual.ErrProvisioned
	}
	hc, series, err := manualDetectSeriesAndHardwareCharacteristics(host)
	if err != nil {
		return nil, err
	}
	finalize := func(ctx environs.BootstrapContext, icfg *instancecfg.InstanceConfig, _ environs.BootstrapDialOpts) error {
		icfg.Bootstrap.BootstrapMachineInstanceId = BootstrapInstanceId
		icfg.Bootstrap.BootstrapMachineHardwareCharacteristics = &hc
		if err := instancecfg.FinishInstanceConfig(icfg, e.Config()); err != nil {
			return err
		}
		return common.ConfigureMachine(ctx, ssh.DefaultClient, host, icfg)
	}

	result := &environs.BootstrapResult{
		Arch:     *hc.Arch,
		Series:   series,
		Finalize: finalize,
	}
	return result, nil
}

// ControllerInstances is specified in the Environ interface.
func (e *manualEnviron) ControllerInstances(controllerUUID string) ([]instance.Id, error) {
	arg0 := filepath.Base(os.Args[0])
	if arg0 != names.Jujud {
		// Not running inside the controller, so we must
		// verify the host.
		if err := e.verifyBootstrapHost(); err != nil {
			return nil, err
		}
	}
	return []instance.Id{BootstrapInstanceId}, nil
}

func (e *manualEnviron) verifyBootstrapHost() error {
	// First verify that the environment is bootstrapped by checking
	// if the agents directory exists. Note that we cannot test the
	// root data directory, as that is created in the process of
	// initialising sshstorage.
	agentsDir := path.Join(agent.DefaultPaths.DataDir, "agents")
	const noAgentDir = "no-agent-dir"
	stdin := fmt.Sprintf(
		"test -d %s || echo %s",
		utils.ShQuote(agentsDir),
		noAgentDir,
	)
	out, err := runSSHCommand(
		"ubuntu@"+e.cfg.bootstrapHost(),
		[]string{"/bin/bash"},
		stdin,
	)
	if err != nil {
		return err
	}
	if out = strings.TrimSpace(out); len(out) > 0 {
		if out == noAgentDir {
			return environs.ErrNotBootstrapped
		}
		err := errors.Errorf("unexpected output: %q", out)
		logger.Infof(err.Error())
		return err
	}
	return nil
}

func (e *manualEnviron) SetConfig(cfg *config.Config) error {
	e.cfgmutex.Lock()
	defer e.cfgmutex.Unlock()
	_, err := manualProvider{}.validate(cfg, e.cfg.Config)
	if err != nil {
		return err
	}
	e.cfg = newModelConfig(cfg, cfg.UnknownAttrs())
	return nil
}

// Implements environs.Environ.
//
// This method will only ever return an Instance for the Id
// BootstrapInstanceId. If any others are specified, then
// ErrPartialInstances or ErrNoInstances will result.
func (e *manualEnviron) Instances(ids []instance.Id) (instances []instance.Instance, err error) {
	instances = make([]instance.Instance, len(ids))
	var found bool
	for i, id := range ids {
		if id == BootstrapInstanceId {
			instances[i] = manualBootstrapInstance{e.envConfig().bootstrapHost()}
			found = true
		} else {
			err = environs.ErrPartialInstances
		}
	}
	if !found {
		err = environs.ErrNoInstances
	}
	return instances, err
}

var runSSHCommand = func(host string, command []string, stdin string) (stdout string, err error) {
	cmd := ssh.Command(host, command, nil)
	cmd.Stdin = strings.NewReader(stdin)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		if stderr := strings.TrimSpace(stderrBuf.String()); len(stderr) > 0 {
			err = errors.Annotate(err, stderr)
		}
		return "", err
	}
	return stdoutBuf.String(), nil
}

func (e *manualEnviron) Destroy() error {
	script := `
set -x
touch %s
pkill -%d jujud && exit
stop %s
rm -f /etc/init/juju*
rm -fr %s %s
exit 0
`
	script = fmt.Sprintf(
		script,
		// WARNING: this is linked with the use of uninstallFile in
		// the agent package. Don't change it without extreme care,
		// and handling for mismatches with already-deployed agents.
		utils.ShQuote(path.Join(
			agent.DefaultPaths.DataDir,
			agent.UninstallFile,
		)),
		terminationworker.TerminationSignal,
		mongo.ServiceName,
		utils.ShQuote(agent.DefaultPaths.DataDir),
		utils.ShQuote(agent.DefaultPaths.LogDir),
	)
	_, err := runSSHCommand(
		"ubuntu@"+e.envConfig().bootstrapHost(),
		[]string{"sudo", "/bin/bash"}, script,
	)
	return err
}

// DestroyController implements the Environ interface.
func (e *manualEnviron) DestroyController(controllerUUID string) error {
	return e.Destroy()
}

func (*manualEnviron) PrecheckInstance(series string, _ constraints.Value, placement string) error {
	return errors.New(`use "juju add-machine ssh:[user@]<host>" to provision machines`)
}

var unsupportedConstraints = []string{
	constraints.CpuPower,
	constraints.InstanceType,
	constraints.Tags,
	constraints.VirtType,
}

// ConstraintsValidator is defined on the Environs interface.
func (e *manualEnviron) ConstraintsValidator() (constraints.Validator, error) {
	validator := constraints.NewValidator()
	validator.RegisterUnsupported(unsupportedConstraints)
	return validator, nil
}

func (e *manualEnviron) OpenPorts(ports []network.PortRange) error {
	return nil
}

func (e *manualEnviron) ClosePorts(ports []network.PortRange) error {
	return nil
}

func (e *manualEnviron) Ports() ([]network.PortRange, error) {
	return nil, nil
}

func (*manualEnviron) Provider() environs.EnvironProvider {
	return manualProvider{}
}
