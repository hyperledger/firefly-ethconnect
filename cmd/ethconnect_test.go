// Copyright 2018 Kaleido, a ConsenSys business

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
