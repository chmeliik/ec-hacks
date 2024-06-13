#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

RH_TITLE="Red Hat Build"
RH_REPO="registry.redhat.io/rhtas/ec-rhel9"
RH_TAGS="${1:-"0.2 0.4 latest"}"

VERBOSE="${VERBOSE:-""}"

UPSTREAM_TITLE="Upstream Build"
UPSTREAM_REPO="quay.io/enterprise-contract/ec-cli"
UPSTREAM_TAG="snapshot"

_show_details() {
	local title="$1"
	local ref="$2"
	local ver="${3/./}"

	# Make sure we have the latest
	podman pull --quiet $ref >/dev/null

	# Get the digest
	local digest="$(skopeo inspect "docker://$ref" | jq -r .Digest)"

	# The likely original Konflux built image
	local konflux_image="quay.io/redhat-user-workloads/rhtap-contract-tenant/ec-$ver/cli-$ver@$digest"

	if [ "$VERBOSE" = "1" ]; then
		# Verbose output
		echo ""
		echo $title
		echo $title | tr '[:print:]' '-'

		echo Ref:
		echo "   $ref"

		echo Pinned ref:
		echo "   $ref@$digest"

		if [[ "$ver" =~ ^v0 ]]; then
			echo Likely Konflux build ref:
			echo "   $konflux_image"
		fi

		echo Version:
		podman run --rm "$ref@$digest" version | sed 's/^/   /'

		echo Binaries in /usr/local/bin:
		podman run --rm --entrypoint /bin/bash "$ref@$digest" -c 'ls -l /usr/local/bin' | sed 's/^/   /'

		echo Command for poking around:
		echo "    podman run --rm -it --entrypoint /bin/bash $ref"

		echo ""

	else
		# Brief output
		echo "$ref@$digest"
		[[ "$ver" =~ ^v0 ]] && echo "$konflux_image"
		podman run --rm "$ref@$digest" version | sed 's/^/   /' | head -3
		echo ""

	fi
}

for t in ${RH_TAGS}; do
	_show_details "$RH_TITLE ($t)" "$RH_REPO:$t" "v$t"
done

_show_details "$UPSTREAM_TITLE" "$UPSTREAM_REPO:$UPSTREAM_TAG" "main-ci"
