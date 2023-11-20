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

package version

import (
	"fmt"
	"runtime"
)

var (
	// Semantic version that refers to ghe version (git tag).
	// For prerelease versions, the build metadata on the
	// semantic version is a git hash some as the gitCommit.
	semVersion = "v0.0.0-master"

	// sha1 from git, output of $(git rev-parse HEAD).
	gitCommit = ""

	// build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ').
	buildDate = "1970-01-01T00:00:00Z"
)

// Version returns the version.
func Version() string {
	return fmt.Sprintf("semVersion %s, gitCommit %s, buildDate %s, goVersion %s, compiler %s, platform %s/%s",
		semVersion, gitCommit, buildDate, runtime.Version(), runtime.Compiler, runtime.GOOS, runtime.GOARCH)
}
