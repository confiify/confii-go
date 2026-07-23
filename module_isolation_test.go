package confii

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCoreModuleExcludesCloudSDKs guards the multi-module boundary: cloud
// integrations live in nested modules, so the root library's go.mod/go.sum
// must never acquire a provider SDK. This keeps core consumers lightweight
// while allowing every module to remain valid under `go mod tidy`.
func TestCoreModuleExcludesCloudSDKs(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Skipf("cannot locate repository root: %v", err)
	}

	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(root, name)
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}

		var offenders []string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			for _, prefix := range cloudModulePrefixes {
				if strings.HasPrefix(line, prefix) {
					offenders = append(offenders, line)
					break
				}
			}
		}
		scanErr := scanner.Err()
		closeErr := f.Close()
		if scanErr != nil {
			t.Fatalf("scan %s: %v", path, scanErr)
		}
		if closeErr != nil {
			t.Fatalf("close %s: %v", path, closeErr)
		}
		if len(offenders) != 0 {
			t.Fatalf("core %s contains cloud SDK modules:\n  %s", name, strings.Join(offenders, "\n  "))
		}
	}
}

var cloudModulePrefixes = []string{
	"cloud.google.com/go",
	"github.com/Azure/azure-sdk-for-go",
	"github.com/AzureAD/microsoft-authentication-library-for-go",
	"github.com/IBM/ibm-cos-sdk-go",
	"github.com/IBM/go-sdk-core",
	"github.com/aws/aws-sdk-go-v2",
	"github.com/aws/smithy-go",
	"github.com/hashicorp/vault/api",
	"google.golang.org/api ",
	"google.golang.org/grpc ",
}

func repositoryRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	return filepath.Dir(file), nil
}
