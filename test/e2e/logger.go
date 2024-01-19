// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 The Falco Authors
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

package e2e

import (
	"fmt"
	"io"

	terratestlogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/testing"
)

// NewLogger returns a new logger for terratest modules.
func NewLogger(writer io.Writer) terratestlogger.TestLogger {
	return &Logger{
		writer: writer,
	}
}

// Logger implements the terratestlogger.TestLogger.
type Logger struct {
	writer io.Writer
}

// Logf prints the logging message.
func (l Logger) Logf(t testing.TestingT, format string, args ...interface{}) {
	terratestlogger.DoLog(t, 3, l.writer, fmt.Sprintf(format, args...))
}
