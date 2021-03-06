// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelconfig

import (
	"github.com/juju/errors"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/state"
)

func init() {
	common.RegisterStandardFacade("ModelConfig", 1, newFacade)
}

func newFacade(st *state.State, _ facade.Resources, auth facade.Authorizer) (*ModelConfigAPI, error) {
	return NewModelConfigAPI(NewStateBackend(st), auth)
}

// ModelConfigAPI is the endpoint which implements the model config facade.
type ModelConfigAPI struct {
	backend Backend
	auth    facade.Authorizer
	check   *common.BlockChecker
}

// NewModelConfigAPI creates a new instance of the ModelConfig Facade.
func NewModelConfigAPI(backend Backend, authorizer facade.Authorizer) (*ModelConfigAPI, error) {
	if !authorizer.AuthClient() {
		return nil, common.ErrPerm
	}
	client := &ModelConfigAPI{
		backend: backend,
		auth:    authorizer,
		check:   common.NewBlockChecker(backend),
	}
	return client, nil
}

// ModelGet implements the server-side part of the
// get-model-config CLI command.
func (c *ModelConfigAPI) ModelGet() (params.ModelConfigResults, error) {
	result := params.ModelConfigResults{}
	values, err := c.backend.ModelConfigValues()
	if err != nil {
		return result, err
	}

	// TODO(wallyworld) - this can be removed once credentials are properly
	// managed outside of model config.
	// Strip out any model config attributes that are credential attributes.
	provider, err := environs.Provider(values[config.TypeKey].Value.(string))
	if err != nil {
		return result, err
	}
	credSchemas := provider.CredentialSchemas()
	var allCredentialAttributes []string
	for _, schema := range credSchemas {
		for _, attr := range schema {
			allCredentialAttributes = append(allCredentialAttributes, attr.Name)
		}
	}
	isCredentialAttribute := func(attr string) bool {
		for _, a := range allCredentialAttributes {
			if a == attr {
				return true
			}
		}
		return false
	}

	result.Config = make(map[string]params.ConfigValue)
	for attr, val := range values {
		if isCredentialAttribute(attr) {
			continue
		}
		// Authorized keys are able to be listed using
		// juju ssh-keys and including them here just
		// clutters everything.
		if attr == config.AuthorizedKeysKey {
			continue
		}
		result.Config[attr] = params.ConfigValue{
			Value:  val.Value,
			Source: val.Source,
		}
	}
	return result, nil
}

// ModelSet implements the server-side part of the
// set-model-config CLI command.
func (c *ModelConfigAPI) ModelSet(args params.ModelSet) error {
	if err := c.check.ChangeAllowed(); err != nil {
		return errors.Trace(err)
	}
	// Make sure we don't allow changing agent-version.
	checkAgentVersion := func(updateAttrs map[string]interface{}, removeAttrs []string, oldConfig *config.Config) error {
		if v, found := updateAttrs["agent-version"]; found {
			oldVersion, _ := oldConfig.AgentVersion()
			if v != oldVersion.String() {
				return errors.New("agent-version cannot be changed")
			}
		}
		return nil
	}
	// Replace any deprecated attributes with their new values.
	attrs := config.ProcessDeprecatedAttributes(args.Config)
	// TODO(waigani) 2014-3-11 #1167616
	// Add a txn retry loop to ensure that the settings on disk have not
	// changed underneath us.
	return c.backend.UpdateModelConfig(attrs, nil, checkAgentVersion)
}

// ModelUnset implements the server-side part of the
// set-model-config CLI command.
func (c *ModelConfigAPI) ModelUnset(args params.ModelUnset) error {
	if err := c.check.ChangeAllowed(); err != nil {
		return errors.Trace(err)
	}
	// TODO(waigani) 2014-3-11 #1167616
	// Add a txn retry loop to ensure that the settings on disk have not
	// changed underneath us.
	return c.backend.UpdateModelConfig(nil, args.Keys, nil)
}
