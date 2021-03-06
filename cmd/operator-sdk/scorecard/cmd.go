// Copyright 2019 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scorecard

import (
	"bytes"
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/mitchellh/mapstructure"
	"github.com/operator-framework/operator-sdk/internal/scorecard"
	schelpers "github.com/operator-framework/operator-sdk/internal/scorecard/helpers"
	scplugins "github.com/operator-framework/operator-sdk/internal/scorecard/plugins"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	configOpt       = "config"
	outputFormatOpt = "output"
	selectorOpt     = "selector"
	bundleOpt       = "bundle"
	listOpt         = "list"
)

var (
	logReadWriter io.ReadWriter
)

func NewCmd() *cobra.Command {

	c := scorecard.Config{}

	scorecardCmd := &cobra.Command{
		Use:   "scorecard",
		Short: "Run scorecard tests",
		Long: `Runs blackbox scorecard tests on an operator
`,
		//RunE: scorecard.Tests,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			buildScorecardConfig(&c)
			err := c.RunTests()
			return err
		},
	}
	scorecardCmd.Flags().String(configOpt, "", fmt.Sprintf("config file (default is '<project_dir>/%s'; the config file's extension and format can be .yaml, .json, or .toml)", scorecard.DefaultConfigFile))
	scorecardCmd.Flags().String(scplugins.KubeconfigOpt, "", "Path to kubeconfig of custom resource created in cluster")
	scorecardCmd.Flags().StringP(outputFormatOpt, "o", scorecard.TextOutputFormat, fmt.Sprintf("Output format for results. Valid values: %s, %s", scorecard.TextOutputFormat, scorecard.JSONOutputFormat))
	scorecardCmd.Flags().String(schelpers.VersionOpt, schelpers.DefaultScorecardVersion, "scorecard version. Valid values: v1alpha1, v1alpha2")
	scorecardCmd.Flags().StringP(selectorOpt, "l", "", "selector (label query) to filter tests on (only valid when version is v1alpha2)")
	scorecardCmd.Flags().BoolP(listOpt, "L", false, "If true, only print the test names that would be run based on selector filtering (only valid when version is v1alpha2)")
	scorecardCmd.Flags().StringP(bundleOpt, "b", "", "OLM bundle directory path, when specified runs bundle validation")

	if err := viper.BindPFlag(configOpt, scorecardCmd.Flags().Lookup(configOpt)); err != nil {
		log.Fatalf("Unable to add config :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+scplugins.KubeconfigOpt, scorecardCmd.Flags().Lookup(scplugins.KubeconfigOpt)); err != nil {
		log.Fatalf("Unable to add kubeconfig :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+outputFormatOpt, scorecardCmd.Flags().Lookup(outputFormatOpt)); err != nil {
		log.Fatalf("Unable to add output format :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+schelpers.VersionOpt, scorecardCmd.Flags().Lookup(schelpers.VersionOpt)); err != nil {
		log.Fatalf("Unable to add version :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+selectorOpt, scorecardCmd.Flags().Lookup(selectorOpt)); err != nil {
		log.Fatalf("Unable to add selector :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+listOpt, scorecardCmd.Flags().Lookup(listOpt)); err != nil {
		log.Fatalf("Unable to add list :%v", err)
	}
	if err := viper.BindPFlag("scorecard."+bundleOpt, scorecardCmd.Flags().Lookup(bundleOpt)); err != nil {
		log.Fatalf("Unable to add bundle :%v", err)
	}

	return scorecardCmd
}

func initConfig() (*viper.Viper, error) {
	// viper/cobra already has flags parsed at this point; we can check if a config file flag is set
	if viper.GetString(configOpt) != "" {
		// Use config file from the flag.
		viper.SetConfigFile(viper.GetString(configOpt))
	} else {
		viper.AddConfigPath(projutil.MustGetwd())
		// using SetConfigName allows users to use a .yaml, .json, or .toml file
		viper.SetConfigName(scorecard.DefaultConfigFile)
	}

	var scViper *viper.Viper
	if err := viper.ReadInConfig(); err == nil {
		scViper = viper.Sub("scorecard")
		// this is a workaround for the fact that nested flags don't persist on viper.Sub
		scViper.Set(outputFormatOpt, viper.GetString("scorecard."+outputFormatOpt))
		scViper.Set(scplugins.KubeconfigOpt, viper.GetString("scorecard."+scplugins.KubeconfigOpt))
		scViper.Set(schelpers.VersionOpt, viper.GetString("scorecard."+schelpers.VersionOpt))
		scViper.Set(selectorOpt, viper.GetString("scorecard."+selectorOpt))
		scViper.Set(bundleOpt, viper.GetString("scorecard."+bundleOpt))
		scViper.Set(listOpt, viper.GetString("scorecard."+listOpt))
		// configure logger output before logging anything
		if !scViper.IsSet(outputFormatOpt) {
			scViper.Set(outputFormatOpt, scorecard.TextOutputFormat)
		}
		format := scViper.GetString(outputFormatOpt)
		if format == scorecard.TextOutputFormat {
			logReadWriter = os.Stdout
		} else if format == scorecard.JSONOutputFormat {
			logReadWriter = &bytes.Buffer{}
		} else {
			return nil, fmt.Errorf("invalid output format: %s", format)
		}
		scorecard.Log.SetOutput(logReadWriter)
		scorecard.Log.Info("Using config file: ", viper.ConfigFileUsed())
	} else {
		return nil, fmt.Errorf("could not read config file: %v\nSee %s for more information about the scorecard config file", err, scorecard.ConfigDocLink())
	}
	return scViper, nil
}

func buildScorecardConfig(c *scorecard.Config) {

	scViper, err := initConfig()
	if err != nil {
		log.Fatalf("%v", err.Error())
	}

	outputFormat := scViper.GetString(outputFormatOpt)
	if outputFormat != scorecard.TextOutputFormat && outputFormat != scorecard.JSONOutputFormat {
		log.Fatalf("Invalid output format (%s); valid values: %s, %s", outputFormat, scorecard.TextOutputFormat, scorecard.JSONOutputFormat)
	}

	version := scViper.GetString(schelpers.VersionOpt)
	err = schelpers.ValidateVersion(version)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if !schelpers.IsV1alpha2(version) && scViper.GetBool(listOpt) {
		log.Fatal("List flag is not supported on v1alpha1")
	}

	c.List = scViper.GetBool(listOpt)
	c.OutputFormat = scViper.GetString(outputFormatOpt)
	c.Version = scViper.GetString(schelpers.VersionOpt)
	c.Bundle = scViper.GetString(bundleOpt)

	if scViper.IsSet(scplugins.KubeconfigOpt) {
		c.Kubeconfig = scViper.GetString(scplugins.KubeconfigOpt)
	}

	c.Selector, err = labels.Parse(scViper.GetString(selectorOpt))
	if err != nil {
		log.Fatalf("%v", err)
	}

	c.PluginConfigs = []scorecard.PluginConfig{}
	if err := scViper.UnmarshalKey("plugins", &c.PluginConfigs, func(c *mapstructure.DecoderConfig) { c.ErrorUnused = true }); err != nil {
		log.Fatalf("%v", errors.Wrap(err, "Could not load plugin configurations"))
	}

	c.Plugins, err = c.GetPlugins(c.PluginConfigs)
	if err != nil {
		log.Fatalf("%v", err)
	}

	c.LogReadWriter = logReadWriter

}
