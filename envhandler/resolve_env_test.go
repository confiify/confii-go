// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package envhandler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

	got := ResolveEnv("", false, "", "")
	assert.Equal(t, "", got, "documented default with nothing set must be the empty string")
}

func TestResolveEnv_FallbackHonoredWhenOSVarUnset(t *testing.T) {

	t.Setenv("CONFII_TEST_G02_OS", "")

	got := ResolveEnv("", false, "CONFII_TEST_G02_OS", "selfcfg-default")
	assert.Equal(t, "selfcfg-default", got)
}

func TestResolveEnv_ExplicitEmptyStringIsRespected(t *testing.T) {

	t.Setenv("CONFII_TEST_G02_OS", "staging")

	got := ResolveEnv("", true, "CONFII_TEST_G02_OS", "selfcfg-default")
	assert.Equal(t, "", got, "explicit flag must dominate even when the explicit value is empty")
}

func TestResolveEnv_RegressionG02_ExplicitProdBeatsOSStaging(t *testing.T) {
	t.Setenv("APP_ENV", "staging")

	got := ResolveEnv("prod", true, "APP_ENV", "")
	assert.Equal(t, "prod", got, " regression:explicit prod must not be overwritten by OS env staging")
}
