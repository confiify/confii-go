package envhandler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// G02: precedence rules for environment selection.
//
// Documented contract: explicit WithEnv > WithEnvSwitcher OS variable >
// self-config default_environment > empty string. The pre-fix bug was
// that the OS-variable lookup ran unconditionally and overwrote any
// explicitly set env. Each test below pins one rung of the precedence
// ladder and would have failed against the pre-fix unconditional-override
// implementation if applied to its logic.

func TestResolveEnv_ExplicitBeatsOSVar(t *testing.T) {
	t.Setenv("CONFII_TEST_G02_OS", "staging")

	got := ResolveEnv("prod", true, "CONFII_TEST_G02_OS", "")
	assert.Equal(t, "prod", got, "explicit WithEnv must win even when the OS variable is set")
}

func TestResolveEnv_OSVarUsedWhenNotExplicit(t *testing.T) {
	t.Setenv("CONFII_TEST_G02_OS", "staging")

	got := ResolveEnv("", false, "CONFII_TEST_G02_OS", "")
	assert.Equal(t, "staging", got, "OS variable must apply when no explicit env was supplied")
}

func TestResolveEnv_ExplicitWinsWhenNoOSVar(t *testing.T) {
	got := ResolveEnv("prod", true, "", "")
	assert.Equal(t, "prod", got)
}

func TestResolveEnv_FallbackWhenNothingSet(t *testing.T) {
	// No explicit env, no OS-var name, no fallback → empty string.
	got := ResolveEnv("", false, "", "")
	assert.Equal(t, "", got, "documented default with nothing set must be the empty string")
}

func TestResolveEnv_FallbackHonoredWhenOSVarUnset(t *testing.T) {
	// OS-var name supplied but the variable itself is empty/unset →
	// fall back to the self-config-style default.
	t.Setenv("CONFII_TEST_G02_OS", "")

	got := ResolveEnv("", false, "CONFII_TEST_G02_OS", "selfcfg-default")
	assert.Equal(t, "selfcfg-default", got)
}

func TestResolveEnv_ExplicitEmptyStringIsRespected(t *testing.T) {
	// An explicit empty string ("I want no environment") must beat the
	// OS variable, because the explicit flag — not the value — is what
	// signals deliberate caller intent.
	t.Setenv("CONFII_TEST_G02_OS", "staging")

	got := ResolveEnv("", true, "CONFII_TEST_G02_OS", "selfcfg-default")
	assert.Equal(t, "", got, "explicit flag must dominate even when the explicit value is empty")
}

// Regression: the exact bug described in the G02 audit text.
// "WithEnvironment(\"prod\")" + OS var set to "staging" → result must be "prod".
func TestResolveEnv_RegressionG02_ExplicitProdBeatsOSStaging(t *testing.T) {
	t.Setenv("APP_ENV", "staging")

	got := ResolveEnv("prod", true, "APP_ENV", "")
	assert.Equal(t, "prod", got, "G02 regression: explicit prod must not be overwritten by OS env staging")
}
