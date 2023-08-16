/*
Copyright 2020 The cert-manager Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cert-manager/cert-manager/controller-binary/app/options"
	config "github.com/cert-manager/cert-manager/internal/apis/config/controller"
	cmdutil "github.com/cert-manager/cert-manager/internal/cmd/util"

	_ "github.com/cert-manager/cert-manager/pkg/controller/acmechallenges"
	_ "github.com/cert-manager/cert-manager/pkg/controller/acmeorders"
	_ "github.com/cert-manager/cert-manager/pkg/controller/certificate-shim/gateways"
	_ "github.com/cert-manager/cert-manager/pkg/controller/certificate-shim/ingresses"
	_ "github.com/cert-manager/cert-manager/pkg/controller/certificates/trigger"
	_ "github.com/cert-manager/cert-manager/pkg/controller/clusterissuers"
	controllerconfigfile "github.com/cert-manager/cert-manager/pkg/controller/configfile"
	_ "github.com/cert-manager/cert-manager/pkg/controller/issuers"
	_ "github.com/cert-manager/cert-manager/pkg/issuer/acme"
	_ "github.com/cert-manager/cert-manager/pkg/issuer/ca"
	_ "github.com/cert-manager/cert-manager/pkg/issuer/selfsigned"
	_ "github.com/cert-manager/cert-manager/pkg/issuer/vault"
	_ "github.com/cert-manager/cert-manager/pkg/issuer/venafi"
	logf "github.com/cert-manager/cert-manager/pkg/logs"
	"github.com/cert-manager/cert-manager/pkg/util"
	"github.com/cert-manager/cert-manager/pkg/util/configfile"
	utilfeature "github.com/cert-manager/cert-manager/pkg/util/feature"
)

const componentController = "controller"

func NewServerCommand(stopCh <-chan struct{}) *cobra.Command {
	ctx := cmdutil.ContextWithStopCh(context.Background(), stopCh)
	log := logf.Log
	ctx = logf.NewContext(ctx, log)

	return newServerCommand(ctx, func(ctx context.Context, cfg *config.ControllerConfiguration) error {
		return Run(cfg, ctx.Done())
	}, os.Args[1:])
}

func newServerCommand(
	ctx context.Context,
	run func(context.Context, *config.ControllerConfiguration) error,
	allArgs []string,
) *cobra.Command {
	log := logf.FromContext(ctx, componentController)

	controllerFlags := options.NewControllerFlags()
	controllerConfig, err := options.NewControllerConfiguration()
	if err != nil {
		log.Error(err, "Failed to create new controller configuration")
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   componentController,
		Short: fmt.Sprintf("Automated TLS controller for Kubernetes (%s) (%s)", util.AppVersion, util.AppGitCommit),
		Long: `
cert-manager is a Kubernetes addon to automate the management and issuance of
TLS certificates from various issuing sources.

It will ensure certificates are valid and up to date periodically, and attempt
to renew certificates at an appropriate time before expiry.`,

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := loadConfigFromFile(
				cmd, allArgs, controllerFlags.Config, controllerConfig,
				func() error {
					if err := logf.ValidateAndApply(&controllerConfig.Logging); err != nil {
						return fmt.Errorf("failed to validate controller logging flags: %w", err)
					}

					// set feature gates from initial flags-based config
					if err := utilfeature.DefaultMutableFeatureGate.SetFromMap(controllerConfig.FeatureGates); err != nil {
						return fmt.Errorf("failed to set feature gates from initial flags-based config: %w", err)
					}

					return nil
				},
			); err != nil {
				return err
			}

			return run(ctx, controllerConfig)
		},
	}

	controllerFlags.AddFlags(cmd.Flags())
	options.AddConfigFlags(cmd.Flags(), controllerConfig)

	// explicitly set provided args in case it does not equal os.Args[:1],
	// eg. when running tests
	cmd.SetArgs(allArgs)

	return cmd
}

func loadConfigFromFile(
	cmd *cobra.Command,
	allArgs []string,
	configFilePath string,
	cfg *config.ControllerConfiguration,
	fn func() error,
) error {
	if err := fn(); err != nil {
		return err
	}

	if len(configFilePath) > 0 {
		// compute absolute path based on current working dir
		controllerConfigFile, err := filepath.Abs(configFilePath)
		if err != nil {
			return fmt.Errorf("failed to load config file %s, error %v", configFilePath, err)
		}

		loader, err := configfile.NewConfigurationFSLoader(nil, controllerConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load config file %s, error %v", configFilePath, err)
		}

		controllerConfigFromFile := controllerconfigfile.New()
		if err := loader.Load(controllerConfigFromFile); err != nil {
			return fmt.Errorf("failed to load config file %s, error %v", configFilePath, err)
		}

		controllerConfigFromFile.Config.DeepCopyInto(cfg)

		_, args, err := cmd.Root().Find(allArgs)
		if err != nil {
			return fmt.Errorf("failed to re-parse flags: %w", err)
		}

		if err := cmd.ParseFlags(args); err != nil {
			return fmt.Errorf("failed to re-parse flags: %w", err)
		}

		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}
