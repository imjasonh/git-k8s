#!/usr/bin/env bash

# Copyright 2024 The git-k8s Authors.
# SPDX-License-Identifier: Apache-2.0

# This script runs the Kubernetes code generators for our custom resources.
# In a full setup, this would invoke deepcopy-gen, client-gen, lister-gen,
# informer-gen, and Knative's injection-gen.
#
# For now, the deepcopy functions are hand-written in zz_generated.deepcopy.go.
# This script serves as the entrypoint for when full code generation is wired up.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
MODULE="github.com/imjasonh/git-k8s"
API_GROUP="git"
API_VERSION="v1alpha1"

echo "Code generation for ${MODULE}/pkg/apis/${API_GROUP}/${API_VERSION}"
echo ""
echo "Currently using hand-written deepcopy and typed client."
echo "To enable full Knative injection-based code generation:"
echo "  1. Install k8s.io/code-generator"
echo "  2. Install knative.dev/pkg/codegen"
echo "  3. Uncomment the generation commands below"
echo ""

# Uncomment when full code-gen tooling is installed:
# bash "${SCRIPT_ROOT}/vendor/k8s.io/code-generator/generate-groups.sh" \
#   "deepcopy,client,informer,lister" \
#   ${MODULE}/pkg/client \
#   ${MODULE}/pkg/apis \
#   "${API_GROUP}:${API_VERSION}" \
#   --go-header-file "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

# Knative injection generation:
# bash "${SCRIPT_ROOT}/vendor/knative.dev/pkg/codegen/cmd/injection-gen" \
#   --input-dirs ${MODULE}/pkg/apis/${API_GROUP}/${API_VERSION} \
#   --output-package ${MODULE}/pkg/client/injection \
#   --go-header-file "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

echo "Done."
