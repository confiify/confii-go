#!/bin/sh

# Verify cloud-tagged packages from the same module shape an actual consumer
# uses. The core and cloud modules are replaced with their local checkouts;
# cloud SDK versions come from the cloud modules themselves.

set -eu

tags=${1:-aws,azure,gcp,vault,ibm}
mode=${2:-test}
repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/confii-cloud-consumer.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
GOCACHE="$fixture_dir/go-build"
export GOCACHE

cd "$fixture_dir"
go mod init confii-cloud-consumer >/dev/null
go mod edit -go=1.25.0
go mod edit \
	-require=github.com/confiify/confii-go@v0.0.0 \
	-require=github.com/confiify/confii-go/loader/cloud@v0.0.0 \
	-require=github.com/confiify/confii-go/secret/cloud@v0.0.0 \
	-require=github.com/confiify/confii-go/examples/cloud@v0.0.0 \
	-replace="github.com/confiify/confii-go=$repo_root" \
	-replace="github.com/confiify/confii-go/loader/cloud=$repo_root/loader/cloud" \
	-replace="github.com/confiify/confii-go/secret/cloud=$repo_root/secret/cloud" \
	-replace="github.com/confiify/confii-go/examples/cloud=$repo_root/examples/cloud"

has_tag() {
	case ",$tags," in
		*,"$1",*) return 0 ;;
		*) return 1 ;;
	esac
}

# Populate the consumer fixture's go.sum, including transitive cloud SDKs.
go mod download all

packages="github.com/confiify/confii-go/loader/cloud"
if has_tag aws || has_tag azure || has_tag gcp || has_tag vault; then
	packages="$packages github.com/confiify/confii-go/secret/cloud"
fi
packages="$packages github.com/confiify/confii-go/examples/cloud/..."

case "$mode" in
	build)
		# shellcheck disable=SC2086 # package list is intentionally word-split.
		go build -tags "$tags" $packages
		;;
	test)
		# shellcheck disable=SC2086 # package list is intentionally word-split.
		go test -tags "$tags" -count=1 -timeout=180s $packages
		;;
	vet)
		# shellcheck disable=SC2086 # package list is intentionally word-split.
		go vet -tags "$tags" $packages
		;;
	vuln)
		# shellcheck disable=SC2086 # package list is intentionally word-split.
		govulncheck -tags "$tags" $packages
		;;
	*)
		echo "usage: $0 [tags] [build|test|vet|vuln]" >&2
		exit 2
		;;
esac
