/*
 * Copyright 2018, CS Systemes d'Information, http://www.c-s.fr
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package services

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"text/template"

	"github.com/CS-SI/SafeScale/providers"
	"github.com/GeertJohan/go.rice"
)

//go:generate rice embed-go

// Return the script (embeded in a rice-box) with placeholders replaced by the values given in data
func getBoxContent(script string, data interface{}) (string, error) {

	box, err := rice.FindBox("broker_scripts")
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return "", tbr
	}
	scriptContent, err := box.String(script)
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return "", tbr
	}
	tpl, err := template.New("TemplateName").Parse(scriptContent)
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return "", tbr
	}

	var buffer bytes.Buffer
	if err = tpl.Execute(&buffer, data); err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return "", tbr
	}

	tplcmd := buffer.String()
	fmt.Println(tplcmd)
	return tplcmd, nil
}

// Execute the given script (embeded in a rice-box) wit the given data on the host identified by hostid
func exec(script string, data interface{}, hostid string, provider *providers.Service) error {
	scriptCmd, err := getBoxContent(script, data)
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return tbr
	}
	// retrieve ssh config to perform some commands
	ssh, err := provider.GetSSHConfig(hostid)
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return tbr
	}

	cmd, err := ssh.SudoCommand(scriptCmd)
	if err != nil {
		// TODO Use more explicit error
		tbr := errors.Wrap(err, "")
		log.Errorf("%+v", tbr)
		return tbr
	}
	_, err = cmd.Output()

	tbr := errors.Wrap(err, "")
	log.Printf("%+v", tbr)
	return tbr
}
