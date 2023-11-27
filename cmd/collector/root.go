// SPDX-License-Identifier: Apache-2.0
// Copyright 2023 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collector

import (
	"context"

	"github.com/falcosecurity/k8s-metacollector/cmd/collector/run"
	"github.com/falcosecurity/k8s-metacollector/cmd/collector/version"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

// New returns the root command.
func New(ctx context.Context, logger *logr.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:              "k8s-metacollector",
		Short:            "Fetches the metadata from kubernetes API server and dispatches them to Falco instances",
		SilenceErrors:    true,
		SilenceUsage:     true,
		TraverseChildren: true,
	}

	cmd.AddCommand(run.New(ctx, logger))
	cmd.AddCommand(version.New())
	return cmd
}
