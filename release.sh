#!/bin/bash
# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


version=`git describe --exact-match --tags`

if [[ -z "$version" ]] 
then
    echo "not tag found. Run this command within a git tag"
    echo "Create a tag for vX.Y.Z and push it to github. Run:"
    echo "git tag vX.Y.Z"
    echo "git push origin vX.Y.Z"
    echo "current tags:"
    git tag
    exit -1
fi

go clean

amdfile=visus-${version}.linux-amd64.tgz
armfile=visus-${version}.linux-arm64.tgz

echo "Checking for license"
go run github.com/google/addlicense -check .

echo "Running test"
go clean -testcache
go test ./...

echo "Running lint"
go run github.com/cockroachdb/crlfmt .
go run golang.org/x/lint/golint -set_exit_status ./...
go run honnef.co/go/tools/cmd/staticcheck -checks all ./...

VERSION="github.com/cockroachlabs/visus/internal/http.Version=$version"
GOOS=linux go build -v  -ldflags="-X '$VERSION'" -o visus

tar cvfz $amdfile  visus  examples

shasum -a 256 $amdfile
GOOS=linux GOARCH=arm64 go build -v  -ldflags="-X '$VERSION'" -o visus

tar cvfz $armfile visus  examples
shasum -a 256 $armfile

go build -ldflags="-X '$VERSION'" -o visus

echo "Upload binaries $amdfile and  $armfile to release"
