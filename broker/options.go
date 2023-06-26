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

package broker

type options struct {
	address               string
	tlsServerCertFilePath string
	tlsServerKeyFilePath  string
}

// Option function used to set options when creating a new Broker instance.
type Option func(opt *options)

// WithTLS configures the grpc server started by the broker to use TLS.
func WithTLS(certFilePath, keyFilePath string) Option {
	return func(opt *options) {
		opt.tlsServerCertFilePath = certFilePath
		opt.tlsServerKeyFilePath = keyFilePath
	}
}

// WithAddress configures the binding address of the grpc server to the
// given value.
func WithAddress(addr string) Option {
	return func(opt *options) {
		opt.address = addr
	}
}
