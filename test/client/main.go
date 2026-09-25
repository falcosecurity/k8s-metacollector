// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The Falco Authors
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	var (
		serverAddr = flag.String("addr", "localhost:45000", "The server address in the format of host:port.")
		nodeName   = flag.String("node-name", "", "Name of the node used to subscribe.")
		numClients = flag.Int("num-clients", 1, "Number of clients to create.")
		noOutput   = flag.Bool("no-output", false, "When true does not print messages")
	)

	logOpts := zap.Options{
		Development: true,
	}
	logOpts.BindFlags(flag.CommandLine)

	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&logOpts))

	grpcOpts := grpc.WithTransportCredentials(insecure.NewCredentials())
	conn, err := grpc.NewClient(*serverAddr, grpcOpts)
	if err != nil {
		logger.Error(err, "unable to create grpc connection")
		return err
	}

	defer conn.Close()

	wait := sync.WaitGroup{}

	for *numClients > 0 {
		*numClients--
		client := metadata.NewMetadataClient(conn)

		// Start the stream.
		stream, err := client.Watch(context.Background(), &metadata.Selector{
			NodeName: *nodeName,
			ResourceKinds: map[string]string{
				resource.Daemonset:  "",
				resource.Namespace:  "",
				resource.Service:    "",
				resource.Pod:        "",
				resource.ReplicaSet: "",
				resource.Deployment: "",
			},
		})

		if err != nil {
			logger.Error(err, "an error occurred while performing the Watch procedure")
			return err
		}

		wait.Go(func() {
			if !*noOutput {
				fmt.Println("[")
			}
			for {
				in, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					logger.Info("received EOF, exiting")
					return
				}

				if err != nil {
					if !*noOutput {
						fmt.Print("]")
					}
					logger.Error(err, "an error occurred while receiving events")
					return
				}

				data, err := json.MarshalIndent(in, "", "  ")
				if err != nil {
					logger.Error(err, "unable to marshal event", "evt", in.String())
				} else if !*noOutput {
					fmt.Print(string(data))
					fmt.Println(",")
				}
			}
		})
	}

	wait.Wait()
	fmt.Print("]")
	return nil
}
