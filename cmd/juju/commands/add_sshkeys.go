// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"errors"
	"fmt"

	"github.com/juju/cmd"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/cmd/juju/block"
	"github.com/juju/juju/cmd/modelcmd"
)

<<<<<<< HEAD:cmd/juju/commands/add_sshkeys.go
// NewAddKeysCommand is used to add a new ssh key for a user.
func NewAddKeysCommand() cmd.Command {
	return envcmd.Wrap(&addKeysCommand{})
=======
func newAddKeysCommand() cmd.Command {
	return modelcmd.Wrap(&addKeysCommand{})
>>>>>>> upstream/api-command-rename:cmd/juju/commands/authorizedkeys_add.go
}

var addKeysDoc = `
Add new authorised ssh keys to allow the holder of those keys to log on to Juju nodes or machines.
`

// addKeysCommand is used to add a new authorized ssh key for a user.
type addKeysCommand struct {
	SSHKeysBase
	user    string
	sshKeys []string
}

func (c *addKeysCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "add-ssh-key",
		Args:    "<ssh key> [...]",
		Doc:     addKeysDoc,
		Purpose: "add new authorized ssh keys for a Juju user",
		Aliases: []string {"add-ssh-keys"},
	}
}

func (c *addKeysCommand) Init(args []string) error {
	switch len(args) {
	case 0:
		return errors.New("no ssh key specified")
	default:
		c.sshKeys = args
	}
	return nil
}

func (c *addKeysCommand) SetFlags(f *gnuflag.FlagSet) {
	f.StringVar(&c.user, "user", "admin", "the user for which to add the keys")
}

func (c *addKeysCommand) Run(context *cmd.Context) error {
	client, err := c.NewKeyManagerClient()
	if err != nil {
		return err
	}
	defer client.Close()

	results, err := client.AddKeys(c.user, c.sshKeys...)
	if err != nil {
		return block.ProcessBlockedError(err, block.BlockChange)
	}
	for i, result := range results {
		if result.Error != nil {
			fmt.Fprintf(context.Stderr, "cannot add key %q: %v\n", c.sshKeys[i], result.Error)
		}
	}
	return nil
}
