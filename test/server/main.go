// SPDX-License-Identifier: Apache-2.0
// Copyright 2023 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"google.golang.org/grpc"
)

var (
	port     = flag.Int("port", 45000, "The server port")
	testFile = flag.String("file", "", "The test file with events to send")
)

type server struct {
	metadata.UnsafeMetadataServer
	eventArray []metadata.Event
}

func (s *server) Watch(in *metadata.Selector, stream metadata.Metadata_WatchServer) error {
	for i := range s.eventArray {
		if err := stream.Send(&s.eventArray[i]); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	flag.Parse()

	// Read the content of the json file and obtain an array of events
	jsonFile, err := os.Open(*testFile)
	if err != nil {
		log.Fatalf("failed to open json file '%s': %v", *testFile, err)
	}
	server_impl := server{}
	byteValue, _ := ioutil.ReadAll(jsonFile)
	json.Unmarshal(byteValue, &server_impl.eventArray)
	jsonFile.Close()

	// Configure the server to listen on the desired port
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	metadata.RegisterMetadataServer(s, &server_impl)

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Printf("failed to serve: %v", err)
	}
}
