// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"io/ioutil"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestExecuteSetDebug(t *testing.T) {
	assert := assert.New(t)

	unitTestCmdRan := false
	utCmd := &cobra.Command{
		Use: "testExecuteSetDebug",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			unitTestCmdRan = true
			return
		},
	}
	rootCmd.AddCommand(utCmd)

	rootCmd.SetArgs([]string{"testExecuteSetDebug"})
	osExit := Execute()

	assert.Equal(true, unitTestCmdRan, "The unittest command ran")
	assert.Equal(0, osExit, "The process exits with 0")

	assert.Equal(log.InfoLevel, log.GetLevel(), "Info level set as default")

	rootCmd.SetArgs([]string{"testExecuteSetDebug", "-d", "0"})
	Execute()
	assert.Equal(log.ErrorLevel, log.GetLevel(), "Error level set")

	rootCmd.SetArgs([]string{"testExecuteSetDebug", "-d", "2"})
	Execute()
	assert.Equal(log.DebugLevel, log.GetLevel(), "Debug level set")

	rootCmd.SetArgs([]string{"testExecuteSetDebug", "-d", "3"})
	Execute()
	assert.Equal(log.TraceLevel, log.GetLevel(), "Trace level set")

	rootCmd.SetArgs([]string{"testExecuteSetDebug", "-d", "99"})
	Execute()
	assert.Equal(log.DebugLevel, log.GetLevel(), "Debug level set")

}

func TestExecuteFail(t *testing.T) {
	assert := assert.New(t)

	utCmd := &cobra.Command{
		Use: "testExecuteFail",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return fmt.Errorf("bang")
		},
	}
	rootCmd.AddCommand(utCmd)

	rootCmd.SetArgs([]string{"testExecuteFail"})
	osExit := Execute()

	assert.Equal(1, osExit, "The process exits with 1")
}

func TestExecuteServerMissingArgs(t *testing.T) {
	assert := assert.New(t)

	rootCmd.SetArgs([]string{"server"})
	osExit := Execute()

	assert.Equal(1, osExit)
}

func TestExecuteServerMissingFile(t *testing.T) {
	assert := assert.New(t)

	rootCmd.SetArgs([]string{"server", "-f", "missing"})
	osExit := Execute()

	assert.Equal(1, osExit)
}

func TestExecuteServerInvalidYAMLContent(t *testing.T) {
	assert := assert.New(t)

	exampleConfYAML, _ := ioutil.TempFile("", "testYAML")
	defer syscall.Unlink(exampleConfYAML.Name())
	ioutil.WriteFile(exampleConfYAML.Name(), []byte("this is not the YAML you are looking for"), 0644)

	rootCmd.SetArgs([]string{"server", "-f", exampleConfYAML.Name()})
	osExit := Execute()

	assert.Equal(1, osExit)
}

func TestExecuteServerWithYAML(t *testing.T) {
	assert := assert.New(t)

	exampleConfYAML, _ := ioutil.TempFile("", "testYAML")
	defer syscall.Unlink(exampleConfYAML.Name())
	ioutil.WriteFile(exampleConfYAML.Name(), []byte(
		"kafka:\n"+
			"  kbridge1:\n"+
			"    topicIn: in1\n"+
			"    topicOut: out1\n"+
			"    rpc:\n"+
			"      url: http://ethereum1\n"+
			"  kbridge2:\n"+
			"    topicIn: in2\n"+
			"    topicOut: out2\n"+
			"    rpc:\n"+
			"      url: http://ethereum1\n"+
			"webhooks: # legacy naming\n"+
			"  wbridge1:\n"+
			"    http:\n"+
			"      port: 1234\n"+
			"rest:\n"+
			"  wbridge2:\n"+
			"    http:\n"+
			"      port: 5678\n"), 0644)

	rootCmd.SetArgs([]string{"server", "-f", exampleConfYAML.Name()})
	osExit := Execute()
	assert.Equal(0, osExit)

	// Print out the yaml too
	rootCmd.SetArgs([]string{"server", "-f", exampleConfYAML.Name(), "-Y"})
	osExit = Execute()
	assert.Equal(0, osExit)
}

func TestExecuteServerWithJSON(t *testing.T) {
	assert := assert.New(t)

	exampleConfYAML, _ := ioutil.TempFile("", "testJSON")
	defer syscall.Unlink(exampleConfYAML.Name())
	testJSON := "{ \"kafka\": {\n" +
		"  \"kbridge1\": {\n" +
		"    \"topicIn\": \"in1\",\n" +
		"    \"topicOut\": \"out1\",\n" +
		"    \"rpc\": {\n" +
		"      \"url\": \"http://ethereum1\"\n" +
		"      }\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	ioutil.WriteFile(exampleConfYAML.Name(), []byte(testJSON), 0644)

	log.Infof("JSON: %s", testJSON)
	rootCmd.SetArgs([]string{"server", "-t", "json", "-f", exampleConfYAML.Name()})
	osExit := Execute()

	assert.Equal(0, osExit)
}

func TestExecuteServerWithIncompleteKafka(t *testing.T) {
	assert := assert.New(t)

	exampleConfYAML, _ := ioutil.TempFile("", "testJSON")
	defer syscall.Unlink(exampleConfYAML.Name())
	testJSON := "{ \"kafka\": {\n" +
		"  \"kbridge1\": {\n" +
		"    \"topicIn\": \"in1\",\n" +
		"    \"topicOut\": \"out1\"\n" +
		"   }\n" +
		"  }\n" +
		"}\n"
	ioutil.WriteFile(exampleConfYAML.Name(), []byte(testJSON), 0644)

	log.Infof("JSON: %s", testJSON)
	rootCmd.SetArgs([]string{"server", "-t", "json", "-f", exampleConfYAML.Name()})
	osExit := Execute()

	assert.Equal(0, osExit)
}

func TestExecuteServerWithBadYAML(t *testing.T) {
	assert := assert.New(t)

	exampleConfYAML, _ := ioutil.TempFile("", "testYAML")
	defer syscall.Unlink(exampleConfYAML.Name())
	ioutil.WriteFile(exampleConfYAML.Name(), []byte("%"), 0644)

	rootCmd.SetArgs([]string{"server", "-f", exampleConfYAML.Name()})
	osExit := Execute()

	assert.Equal(1, osExit)
}
