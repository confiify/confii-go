// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The six probes below are the third-party review's reproductions of the same
// defect: strict admission maintained an allowlist that had drifted from the
// code consuming the settings. A strict configuration that accepts a setting it
// then ignores is the precise failure strict mode exists to prevent, so each
// disagreement is written down as its own case.

func TestVaultStrict_RejectsVerifyFalseBecauseHermeticCannotHonorIt(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://vault.example.invalid:8200",
		"verify":  false,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.Contains(t, err.Error(), "verify")
}

// Omission and an explicit true both mean "verify", which hermetic mode does.
func TestVaultStrict_AcceptsVerifyTrue(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"omitted": {"strict": true, "address": "https://vault.example.invalid:8200"},
		"true":    {"strict": true, "address": "https://vault.example.invalid:8200", "verify": true},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, admitStrictVaultConfig(cfg))
		})
	}
}

func TestVaultStrict_RejectsAuthKeysTheMethodNeverReads(t *testing.T) {
	for _, key := range []string{"token_env", "token_path"} {
		t.Run(key, func(t *testing.T) {
			_, err := vaultProvider(t, map[string]any{
				"strict":  true,
				"address": "https://vault.example.invalid:8200",
				"auth":    map[string]any{"method": "token", "token": "s.declared", key: "ignored"},
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
			assert.Contains(t, err.Error(), key)
		})
	}
}

// The mirror failure: a setting the method does read was rejected as unknown,
// so a documented and honored option could not be declared at all.
//
// Admission is asserted directly here, not through the provider. A positive
// case that reached construction would go on to authenticate against the
// address it names, and a test that needs a live Vault to prove a schema
// question is testing the wrong thing.
func TestVaultStrict_AdmitsOIDCCallbackTimeout(t *testing.T) {
	assert.NoError(t, admitStrictVaultConfig(map[string]any{
		"strict":  true,
		"address": "https://vault.example.invalid:8200",
		"auth": map[string]any{
			"method": "oidc", "role": "r", "callback_timeout_seconds": 30,
		},
	}))
}

func TestVaultStrict_RejectsConflictingAuthAliases(t *testing.T) {
	for name, auth := range map[string]map[string]any{
		"method vs type": {"method": "token", "type": "ldap", "token": "s.x"},
		"mount vs mount_point": {
			"method": "approle", "role_id": "r", "secret_id": "s",
			"mount": "one", "mount_point": "two",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vaultProvider(t, map[string]any{
				"strict":  true,
				"address": "https://vault.example.invalid:8200",
				"auth":    auth,
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

// Agreeing aliases are not a conflict; one of them is simply redundant.
func TestVaultStrict_AcceptsAgreeingAuthAliases(t *testing.T) {
	assert.NoError(t, admitStrictVaultConfig(map[string]any{
		"strict":  true,
		"address": "https://vault.example.invalid:8200",
		"auth": map[string]any{
			"method": "approle", "type": "approle", "role_id": "r", "secret_id": "s",
			"mount": "same", "mount_point": "same",
		},
	}))
}

// authProbeValue returns a value distinguishable from the zero one for the kind
// of setting key holds, so a probe can tell "consumed" from "ignored".
func authProbeValue(key string) any {
	switch key {
	case "wrapping_token":
		return true
	case "callback_timeout_seconds":
		return 42
	default:
		return "confii-probe-" + key
	}
}

// consumedAuthKeys asks the construction code itself which keys it reads, by
// building the method twice and seeing whether the key changed the result. It
// derives ground truth from behavior rather than from a second list that can
// drift out of step with the first — which is how the six probes above came to
// fail in the first place.
func consumedAuthKeys(t *testing.T, method string, candidates []string) map[string]struct{} {
	t.Helper()
	base, err := buildVaultSelfAuth(method, map[string]any{}, "")
	require.NoError(t, err, "method %q must build from an empty map", method)

	consumed := make(map[string]struct{})
	for _, key := range candidates {
		probed, err := buildVaultSelfAuth(method, map[string]any{key: authProbeValue(key)}, "")
		require.NoError(t, err, "method %q with %q must build", method, key)
		if !reflect.DeepEqual(base, probed) {
			consumed[key] = struct{}{}
		}
	}
	return consumed
}

// TestVaultStrict_SchemaAndConstructionAgree is the bidirectional check the
// review asked for. Every admitted key must be consumed, and every consumed key
// must be admitted, for every supported method. Either direction failing is a
// lie: an admitted-but-unread key is a setting silently ignored, and a
// read-but-unadmitted key is a supported setting that cannot be declared.
func TestVaultStrict_SchemaAndConstructionAgree(t *testing.T) {
	// The vocabulary comes from the construction source, not from the schema
	// under test. Building it from the schema would make the check blind in
	// exactly one direction: a key the construction reads but no schema entry
	// mentions is never probed, and so never reported. That is the hole
	// callback_timeout_seconds fell through.
	universe := keysReadByAuthConstruction(t)
	for _, keys := range vaultStrictAuthMethodKeys {
		for key := range keys {
			universe[key] = struct{}{}
		}
	}
	candidates := make([]string, 0, len(universe))
	for key := range universe {
		// method and type select the method and mount/mount_point are
		// aliases handled by the common set; neither is a method setting.
		if _, common := vaultStrictAuthCommonKeys[key]; common {
			continue
		}
		candidates = append(candidates, key)
	}
	sort.Strings(candidates)

	for method, admitted := range vaultStrictAuthMethodKeys {
		t.Run(method, func(t *testing.T) {
			consumed := consumedAuthKeys(t, method, candidates)

			var ignored, undeclarable []string
			for key := range admitted {
				if _, ok := consumed[key]; !ok {
					ignored = append(ignored, key)
				}
			}
			for key := range consumed {
				if _, ok := admitted[key]; !ok {
					undeclarable = append(undeclarable, key)
				}
			}
			sort.Strings(ignored)
			sort.Strings(undeclarable)

			assert.Empty(t, ignored, fmt.Sprintf(
				"method %q admits keys it never reads; a strict configuration would "+
					"accept them and silently ignore them", method))
			assert.Empty(t, undeclarable, fmt.Sprintf(
				"method %q reads keys it does not admit; they cannot be declared "+
					"under strict mode at all", method))
		})
	}
}

// An alias maps onto its canonical method's key set, so the two must build the
// same thing from the same map. Otherwise the alias admits one vocabulary and
// consumes another.
func TestVaultStrict_AliasesShareTheirCanonicalConstruction(t *testing.T) {
	for alias, canonical := range vaultStrictAuthAliases {
		t.Run(alias+"="+canonical, func(t *testing.T) {
			keys := vaultStrictAuthMethodKeys[canonical]
			cfg := map[string]any{}
			for key := range keys {
				cfg[key] = authProbeValue(key)
			}
			viaAlias, err := buildVaultSelfAuth(alias, cfg, "")
			require.NoError(t, err)
			viaCanonical, err := buildVaultSelfAuth(canonical, cfg, "")
			require.NoError(t, err)
			assert.Equal(t, viaCanonical, viaAlias)
		})
	}
}

// keysReadByAuthConstruction returns every configuration key buildVaultSelfAuth
// looks up, read out of the source itself. It is the authoritative side of the
// contract: the schema is a claim about this set, and a claim needs something
// independent to be checked against.
func keysReadByAuthConstruction(t *testing.T) map[string]struct{} {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "selfconfig_vault.go", nil, 0)
	require.NoError(t, err)

	var body *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if isFunc && fn.Name.Name == "buildVaultSelfAuth" {
			body = fn
			break
		}
	}
	require.NotNil(t, body, "buildVaultSelfAuth must exist for the contract to mean anything")

	keys := make(map[string]struct{})
	ast.Inspect(body, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		fn, isIdent := call.Fun.(*ast.Ident)
		if !isIdent {
			return true
		}
		switch fn.Name {
		case "selfString", "selfBool", "selfInt":
		default:
			return true
		}
		// Argument 0 is the map; every argument after it is a key spelling.
		for _, arg := range call.Args[1:] {
			lit, isLit := arg.(*ast.BasicLit)
			if isLit && lit.Kind == token.STRING {
				keys[strings.Trim(lit.Value, `"`)] = struct{}{}
			}
		}
		return true
	})
	require.NotEmpty(t, keys)
	return keys
}

// The key-presence check above proves admitted names match consumed names. It
// cannot prove an admitted value is usable, because its probes deliberately
// supply the expected type. This is the other half: every recognized setting,
// given a value its kind cannot hold, must be refused rather than accepted and
// dropped.
//
// The matrix is derived from the schema, so a setting added later is covered
// without anyone remembering to add a case.

// wrongValuesFor returns values that the kind cannot represent. Each is a
// concrete shape a configuration file can actually produce.
func wrongValuesFor(kind vaultValueKind) map[string]any {
	switch kind {
	case vaultValueString:
		return map[string]any{
			"a number": 42, "a boolean": true, "a map": map[string]any{"k": "v"},
			"a list": []any{"v"}, "an empty string": "", "only whitespace": "   ",
		}
	case vaultValueBool:
		return map[string]any{
			"a number": 42, "a map": map[string]any{"k": "v"},
			"a list": []any{"v"}, "a non-boolean string": "yes-please",
		}
	case vaultValueInt:
		return map[string]any{
			"a fraction": 1.5, "a boolean": true, "a map": map[string]any{"k": "v"},
			"a list": []any{"v"}, "a non-numeric string": "many",
			"an overflow": 1e300,
		}
	case vaultValueDuration:
		return map[string]any{
			"a number": 30, "a boolean": true, "a bare unit": "s",
			"a map": map[string]any{"k": "v"}, "an empty string": "",
			"a non-duration string": "half an hour",
		}
	case vaultValueMap:
		return map[string]any{
			"a string": "x", "a number": 42, "a boolean": true, "a list": []any{"v"},
		}
	case vaultValueMethodOrMap:
		return map[string]any{
			"a number": 42, "a boolean": true, "a list": []any{"v"},
			"an empty string": "",
		}
	}
	return nil
}

func TestVaultStrict_RejectsRecognizedSettingsWithUnusableValues(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"strict":  true,
			"address": "https://vault.example.invalid:8200",
		}
	}

	t.Run("root", func(t *testing.T) {
		for key, kind := range vaultStrictRootKeys {
			for shape, value := range wrongValuesFor(kind) {
				t.Run(key+"/"+shape, func(t *testing.T) {
					cfg := base()
					cfg[key] = value
					err := admitStrictVaultConfig(cfg)
					require.Error(t, err, "%s = %#v must not be accepted", key, value)
					assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
				})
			}
		}
	})

	t.Run("tls", func(t *testing.T) {
		for key, kind := range vaultStrictTLSKeys {
			for shape, value := range wrongValuesFor(kind) {
				t.Run(key+"/"+shape, func(t *testing.T) {
					cfg := base()
					cfg["tls"] = map[string]any{key: value}
					err := admitStrictVaultConfig(cfg)
					require.Error(t, err)
					assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
				})
			}
		}
	})

	t.Run("auth", func(t *testing.T) {
		for method, keys := range vaultStrictAuthMethodKeys {
			all := map[string]vaultValueKind{}
			for key, kind := range vaultStrictAuthCommonKeys {
				all[key] = kind
			}
			for key, kind := range keys {
				all[key] = kind
			}
			for key, kind := range all {
				// method and type select the method; a wrong value there is
				// covered by the dedicated case below rather than by
				// substitution into a map that names the method already.
				if key == "method" || key == "type" {
					continue
				}
				for shape, value := range wrongValuesFor(kind) {
					t.Run(method+"/"+key+"/"+shape, func(t *testing.T) {
						cfg := base()
						cfg["auth"] = map[string]any{"method": method, key: value}
						err := admitStrictVaultConfig(cfg)
						require.Error(t, err)
						assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
					})
				}
			}
		}
	})

	t.Run("method selector", func(t *testing.T) {
		for shape, value := range wrongValuesFor(vaultValueString) {
			t.Run(shape, func(t *testing.T) {
				cfg := base()
				cfg["auth"] = map[string]any{"method": value}
				err := admitStrictVaultConfig(cfg)
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
			})
		}
	})
}

// Differently typed spellings of one setting are not agreement. fmt.Sprint
// renders true and "true" identically, which is how address: true beside
// url: "true" came to be read as two spellings of the same value.
func TestVaultStrict_TypedAliasesDoNotAgreeByTheirPrintedForm(t *testing.T) {
	err := admitStrictVaultConfig(map[string]any{
		"strict": true, "address": true, "url": "true",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
}

// The complement: every setting given a value of its declared kind must be
// admitted, so the value contract cannot be satisfied by refusing everything.
func TestVaultStrict_AdmitsEverySettingWithAUsableValue(t *testing.T) {
	usable := map[vaultValueKind]any{
		vaultValueString:      "value",
		vaultValueBool:        true,
		vaultValueInt:         2,
		vaultValueDuration:    "30s",
		vaultValueMap:         map[string]any{},
		vaultValueMethodOrMap: "aws",
	}
	for key, kind := range vaultStrictRootKeys {
		if key == "verify" {
			continue // hermetic mode refuses false; true is covered elsewhere
		}
		t.Run(key, func(t *testing.T) {
			cfg := map[string]any{
				"strict":  true,
				"address": "https://vault.example.invalid:8200",
			}
			cfg[key] = usable[kind]
			if key == "url" {
				cfg["address"] = usable[kind] // agreeing aliases
			}
			assert.NoError(t, admitStrictVaultConfig(cfg))
		})
	}
}

// A rejected value must not be echoed. The matrix above cannot assert this
// usefully — an empty string is contained in every message, and "30" appears
// inside the example duration "30s" — so the check uses one distinctive value
// per kind instead, of the shape a real credential has.
func TestVaultStrict_ValueErrorsDoNotEchoTheValue(t *testing.T) {
	const secretish = "s.AbCdEf0123456789-token-shaped-value"
	for name, cfg := range map[string]map[string]any{
		"root string given a map": {
			"strict": true, "address": "https://vault.example.invalid:8200",
			"namespace": map[string]any{"leaked": secretish},
		},
		"root bool given a string": {
			"strict": true, "address": "https://vault.example.invalid:8200",
			"follow_redirects": secretish,
		},
		"tls string given a number": {
			"strict": true, "address": "https://vault.example.invalid:8200",
			"tls": map[string]any{"server_name": []any{secretish}},
		},
		"auth string given a list": {
			"strict": true, "address": "https://vault.example.invalid:8200",
			"auth": map[string]any{"method": "token", "token": []any{secretish}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := admitStrictVaultConfig(cfg)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
			assert.NotContains(t, err.Error(), secretish,
				"a rejected value must never reach the error text")
		})
	}
}

// A usable type is not the same as a declaration that takes effect. These cover
// the rest of the strict invariant: every present declaration is either consumed
// with its declared meaning, rejected as conflicting, or covered by a documented
// default — never accepted and quietly discarded.

func TestVaultStrict_ValidatesBareAuthMethod(t *testing.T) {
	err := admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"auth": "no-such-method",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration,
		"a bare method name is a declaration and its failure is a strict configuration error")
	assert.Contains(t, err.Error(), "no-such-method")
}

func TestVaultStrict_AcceptsBareAuthMethodIncludingAliases(t *testing.T) {
	// Only a method whose contract requires nothing can stand alone. Every
	// other bare name is an incomplete declaration, which is the subject of
	// TestVaultStrict_RejectsEveryIncompleteDeclaration.
	for _, method := range []string{"aws", "aws_iam", "awsiam", "oidc", " OIDC "} {
		t.Run(method, func(t *testing.T) {
			assert.NoError(t, admitStrictVaultConfig(map[string]any{
				"strict": true, "address": "https://vault.example.invalid:8200",
				"auth": method,
			}))
		})
	}
}

// A bare method name cannot carry the credentials its method requires.
func TestVaultStrict_RejectsBareMethodThatNeedsCredentials(t *testing.T) {
	err := admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"auth": "approle",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.Contains(t, err.Error(), "role_id")
}

// A root token is only meaningful when token authentication is what runs. With
// any other method selected it is passed to a constructor that never reads it.
func TestVaultStrict_RejectsRootTokenBesideAnotherAuthMethod(t *testing.T) {
	err := admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"token": "s.root-token",
		"auth":  map[string]any{"method": "approle", "role_id": "r", "secret_id": "s"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.NotContains(t, err.Error(), "s.root-token")
}

// Two spellings of the same credential, where the root one wins and the nested
// one is discarded without a word.
func TestVaultStrict_RejectsDisagreeingRootAndNestedToken(t *testing.T) {
	err := admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"token": "s.root-token",
		"auth":  map[string]any{"method": "token", "token": "s.nested-token"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.NotContains(t, err.Error(), "s.root-token")
	assert.NotContains(t, err.Error(), "s.nested-token")
}

func TestVaultStrict_AcceptsRootTokenWithTokenAuth(t *testing.T) {
	assert.NoError(t, admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"token": "s.root-token", "auth": map[string]any{"method": "token"},
	}))
	assert.NoError(t, admitStrictVaultConfig(map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200",
		"token": "s.same", "auth": map[string]any{"method": "token", "token": "s.same"},
	}))
}

// Zero is not a timeout. Construction converts a non-positive value to the
// default, so declaring it asks for one thing and gets another.
func TestVaultStrict_RejectsNonPositiveOIDCCallbackTimeout(t *testing.T) {
	for _, seconds := range []int{0, -1} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			err := admitStrictVaultConfig(map[string]any{
				"strict": true, "address": "https://vault.example.invalid:8200",
				"auth": map[string]any{
					"method": "oidc", "role": "r", "callback_timeout_seconds": seconds,
				},
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

func TestVaultStrict_EnforcesCredentialSourceExclusivity(t *testing.T) {
	base := func(auth map[string]any) map[string]any {
		return map[string]any{
			"strict": true, "address": "https://vault.example.invalid:8200", "auth": auth,
		}
	}
	t.Run("approle needs exactly one secret id source", func(t *testing.T) {
		none := base(map[string]any{"method": "approle", "role_id": "r"})
		require.ErrorIs(t, admitStrictVaultConfig(none), ErrVaultStrictConfiguration)

		two := base(map[string]any{
			"method": "approle", "role_id": "r",
			"secret_id": "s", "secret_id_env": "VAULT_SECRET_ID",
		})
		require.ErrorIs(t, admitStrictVaultConfig(two), ErrVaultStrictConfiguration)

		for _, source := range []string{"secret_id", "secret_id_file", "secret_id_env"} {
			one := base(map[string]any{"method": "approle", "role_id": "r", source: "value"})
			assert.NoError(t, admitStrictVaultConfig(one), "exactly one %s is valid", source)
		}
	})
	t.Run("kubernetes takes at most one token source", func(t *testing.T) {
		two := base(map[string]any{
			"method": "kubernetes", "role": "r", "jwt": "j", "token_path": "/p",
		})
		require.ErrorIs(t, admitStrictVaultConfig(two), ErrVaultStrictConfiguration)

		assert.NoError(t, admitStrictVaultConfig(
			base(map[string]any{"method": "kubernetes", "role": "r"})),
			"none is valid; the in-cluster default applies")
		for _, source := range []string{"jwt", "token_path", "token_env"} {
			assert.NoError(t, admitStrictVaultConfig(
				base(map[string]any{"method": "kubernetes", "role": "r", source: "value"})))
		}
	})
}

// Domain constraints already existed in construction, but their errors were
// plain, so a caller matching on the strict sentinel did not see them.
func TestVaultStrict_DomainFailuresWrapTheStrictSentinel(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"kv_version too low":   {"kv_version": 0},
		"kv_version too high":  {"kv_version": 3},
		"negative retry_limit": {"retry_limit": -1},
	} {
		t.Run(name, func(t *testing.T) {
			full := map[string]any{
				"strict": true, "address": "https://vault.example.invalid:8200",
			}
			for k, v := range cfg {
				full[k] = v
			}
			err := admitStrictVaultConfig(full)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
	for name, cfg := range map[string]map[string]any{
		"kv_version 1":      {"kv_version": 1},
		"kv_version 2":      {"kv_version": 2},
		"retry_limit zero":  {"retry_limit": 0},
		"retry_limit three": {"retry_limit": 3},
	} {
		t.Run(name, func(t *testing.T) {
			full := map[string]any{
				"strict": true, "address": "https://vault.example.invalid:8200",
			}
			for k, v := range cfg {
				full[k] = v
			}
			assert.NoError(t, admitStrictVaultConfig(full))
		})
	}
}

// minimalValidAuth is the smallest declaration each method can run with. It is
// the fixture for both directions: remove any required field and admission must
// refuse; leave it whole and admission must accept.
func minimalValidAuth() map[string]map[string]any {
	return map[string]map[string]any{
		"token":      {"method": "token", "token": "s.declared"},
		"approle":    {"method": "approle", "role_id": "r", "secret_id": "s"},
		"ldap":       {"method": "ldap", "username": "u", "password": "p"},
		"jwt":        {"method": "jwt", "role": "r", "jwt": "j"},
		"gcp_jwt":    {"method": "gcp_jwt", "role": "r", "jwt": "j"},
		"azure":      {"method": "azure", "role": "r"},
		"azure_jwt":  {"method": "azure_jwt", "role": "r", "jwt": "j"},
		"kubernetes": {"method": "kubernetes", "role": "r"},
		"gcp":        {"method": "gcp", "role": "r"},
		"oidc":       {"method": "oidc"},
		"aws":        {"method": "aws"},
		"aws_signed_request": {
			"method":                  "aws_signed_request",
			"iam_http_request_method": "POST",
			"iam_request_url":         "https://sts.amazonaws.com",
			"iam_request_body":        "Action=GetCallerIdentity",
			"iam_request_headers":     "{}",
		},
	}
}

func strictWithAuth(auth any) map[string]any {
	return map[string]any{
		"strict": true, "address": "https://vault.example.invalid:8200", "auth": auth,
	}
}

// Every method has a declaration it can actually run with. Without this the
// contract could be satisfied by refusing everything.
func TestVaultStrict_AdmitsAMinimalValidDeclarationForEveryMethod(t *testing.T) {
	minimal := minimalValidAuth()
	for method := range vaultStrictAuthMethodKeys {
		t.Run(method, func(t *testing.T) {
			auth, defined := minimal[method]
			require.True(t, defined,
				"every supported method needs a minimal valid declaration here")
			assert.NoError(t, admitStrictVaultConfig(strictWithAuth(auth)))
		})
	}
}

// Removing any single required field must be refused, for every method. The
// cases are derived from the contract, so a requirement added later is covered.
func TestVaultStrict_RejectsEveryIncompleteDeclaration(t *testing.T) {
	minimal := minimalValidAuth()
	for method, contract := range vaultAuthContracts {
		auth, defined := minimal[method]
		require.True(t, defined)

		for _, required := range contract.required {
			t.Run(method+"/without "+required, func(t *testing.T) {
				incomplete := map[string]any{}
				for k, v := range auth {
					if k != required {
						incomplete[k] = v
					}
				}
				err := admitStrictVaultConfig(strictWithAuth(incomplete))
				require.Error(t, err, "%s without %s must not be admitted", method, required)
				assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
				assert.Contains(t, err.Error(), required)
			})
		}

		for _, group := range contract.oneOf {
			if !group.required {
				continue
			}
			t.Run(method+"/without "+group.what, func(t *testing.T) {
				incomplete := map[string]any{}
				for k, v := range auth {
					incomplete[k] = v
				}
				for _, key := range group.keys {
					delete(incomplete, key)
				}
				err := admitStrictVaultConfig(strictWithAuth(incomplete))
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
			})
		}
	}
}

// The five configurations the review reproduced, named individually so a
// regression in any one of them is legible without reading the derived matrix.
func TestVaultStrict_RejectsTheReportedIncompleteConfigurations(t *testing.T) {
	for name, auth := range map[string]any{
		"bare token without a token":     "token",
		"approle without role_id":        map[string]any{"method": "approle", "secret_id": "s"},
		"bare ldap":                      "ldap",
		"bare jwt":                       "jwt",
		"bare kubernetes without a role": "kubernetes",
	} {
		t.Run(name, func(t *testing.T) {
			err := admitStrictVaultConfig(strictWithAuth(auth))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

func TestVaultStrict_GCPAuthTypeIsRestrictedAndConditional(t *testing.T) {
	for _, authType := range []string{"", "gce", "GCE"} {
		t.Run("accepts "+authType, func(t *testing.T) {
			auth := map[string]any{"method": "gcp", "role": "r"}
			if authType != "" {
				auth["auth_type"] = authType
			}
			assert.NoError(t, admitStrictVaultConfig(strictWithAuth(auth)))
		})
	}
	t.Run("iam requires a service account email", func(t *testing.T) {
		err := admitStrictVaultConfig(strictWithAuth(
			map[string]any{"method": "gcp", "role": "r", "auth_type": "iam"}))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		assert.Contains(t, err.Error(), "service_account_email")
	})
	t.Run("iam with one is accepted", func(t *testing.T) {
		assert.NoError(t, admitStrictVaultConfig(strictWithAuth(map[string]any{
			"method": "gcp", "role": "r", "auth_type": "iam",
			"service_account_email": "svc@example.invalid",
		})))
	})
	t.Run("rejects an unsupported auth type", func(t *testing.T) {
		err := admitStrictVaultConfig(strictWithAuth(
			map[string]any{"method": "gcp", "role": "r", "auth_type": "workload"}))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	})
}

// An alias is the same method under another name, so it inherits the same
// contract. Nothing may be declarable through an alias that is refused through
// its canonical spelling.
func TestVaultStrict_AliasesInheritTheirCanonicalContract(t *testing.T) {
	minimal := minimalValidAuth()
	for alias, canonical := range vaultStrictAuthAliases {
		t.Run(alias, func(t *testing.T) {
			valid := map[string]any{}
			for k, v := range minimal[canonical] {
				valid[k] = v
			}
			valid["method"] = alias
			assert.NoError(t, admitStrictVaultConfig(strictWithAuth(valid)),
				"%s must accept what %s accepts", alias, canonical)

			for _, required := range vaultAuthContracts[canonical].required {
				incomplete := map[string]any{}
				for k, v := range valid {
					if k != required {
						incomplete[k] = v
					}
				}
				err := admitStrictVaultConfig(strictWithAuth(incomplete))
				assert.ErrorIs(t, err, ErrVaultStrictConfiguration,
					"%s must refuse what %s refuses (missing %s)", alias, canonical, required)
			}
		})
	}
}

// No contract failure may name a credential.
func TestVaultStrict_ContractErrorsDoNotEchoCredentials(t *testing.T) {
	const secretish = "s.AbCdEf0123456789-token-shaped-value"
	for name, auth := range map[string]map[string]any{
		"approle missing role_id": {"method": "approle", "secret_id": secretish},
		"ldap missing password":   {"method": "ldap", "username": secretish},
		"jwt missing role":        {"method": "jwt", "jwt": secretish},
	} {
		t.Run(name, func(t *testing.T) {
			err := admitStrictVaultConfig(strictWithAuth(auth))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), secretish)
		})
	}
}

// The synchronization check: admission and the auth implementations must agree.
// These constructors validate without contacting anything, so a declaration
// admission accepts must be one they accept, and the incomplete declarations
// admission refuses must be ones they refuse too. Enforcing the rule in two
// places is only safe while the two places say the same thing.
func TestVaultStrict_AdmissionAgreesWithTheAuthImplementations(t *testing.T) {
	minimal := minimalValidAuth()
	// Some constructors do environment work past contract validation:
	// NewKubernetesAuth reads the in-cluster service-account token file when no
	// token source is declared, which does not exist off-cluster. Declaring one
	// keeps this check on the contract, which is what the two sides must agree
	// about, rather than on where the test happens to run.
	minimal["kubernetes"] = map[string]any{
		"method": "kubernetes", "role": "r", "jwt": "declared-token",
	}
	for method, build := range map[string]func(VaultAuthMethod) error{
		"approle": func(a VaultAuthMethod) error {
			_, err := newOfficialAppRoleMethod(a.(*AppRoleAuth))
			return err
		},
		"kubernetes": func(a VaultAuthMethod) error {
			_, err := newOfficialKubernetesMethod(a.(*KubernetesAuth))
			return err
		},
		"azure": func(a VaultAuthMethod) error {
			_, err := newOfficialAzureMethod(a.(*AzureAuth))
			return err
		},
		"gcp": func(a VaultAuthMethod) error {
			_, err := newOfficialGCPMethod(a.(*GCPAuth))
			return err
		},
	} {
		t.Run(method+"/valid is accepted by both", func(t *testing.T) {
			auth := minimal[method]
			require.NoError(t, admitStrictVaultConfig(strictWithAuth(auth)))
			built, err := buildVaultSelfAuth(method, auth, "")
			require.NoError(t, err)
			assert.NoError(t, build(built),
				"admission accepted a declaration the implementation rejects")
		})
		for _, required := range vaultAuthContracts[method].required {
			t.Run(method+"/without "+required+" is refused by both", func(t *testing.T) {
				incomplete := map[string]any{}
				for k, v := range minimal[method] {
					if k != required {
						incomplete[k] = v
					}
				}
				require.ErrorIs(t, admitStrictVaultConfig(strictWithAuth(incomplete)),
					ErrVaultStrictConfiguration)
				built, err := buildVaultSelfAuth(method, incomplete, "")
				require.NoError(t, err)
				assert.Error(t, build(built),
					"admission refused a declaration the implementation would accept; "+
						"the two are no longer describing the same contract")
			})
		}
	}
}
