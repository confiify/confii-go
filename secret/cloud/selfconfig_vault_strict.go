// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Strict mode is a closed schema, and a schema that is closed only at its root
// is not closed. An unrecognised key inside tls or auth was accepted before,
// so a mistyped nested setting silently kept its default — the exact failure
// strict mode exists to prevent, one level down.
//
// Each set below is the complete list of keys its scope understands.
// vaultValueKind is the value contract for a setting: what a declaration may
// hold, not merely whether the name is recognized.
//
// Closing the schema over names alone left half the promise unmet. A recognized
// setting given a value of the wrong type was accepted and then dropped, because
// selfString answers a non-string with the empty string and no error — so
// namespace: 42 configured nothing and said nothing. Admission and construction
// now agree on the value as well as the name.
type vaultValueKind uint8

const (
	vaultValueString vaultValueKind = iota
	vaultValueBool
	vaultValueInt
	vaultValueDuration
	vaultValueMap
	// vaultValueMethodOrMap is auth, which is either a bare method name or a
	// map of settings for that method.
	vaultValueMethodOrMap
)

var (
	vaultStrictRootKeys = map[string]vaultValueKind{
		"strict":           vaultValueBool,
		"address":          vaultValueString,
		"url":              vaultValueString,
		"namespace":        vaultValueString,
		"mount_point":      vaultValueString,
		"mount":            vaultValueString,
		"kv_version":       vaultValueInt,
		"verify":           vaultValueBool,
		"token":            vaultValueString,
		"auth":             vaultValueMethodOrMap,
		"timeout":          vaultValueDuration,
		"retry_limit":      vaultValueInt,
		"proxy":            vaultValueString,
		"tls":              vaultValueMap,
		"follow_redirects": vaultValueBool,
	}

	vaultStrictTLSKeys = map[string]vaultValueKind{
		"ca_cert_pem":     vaultValueString,
		"client_cert_pem": vaultValueString,
		"client_key_pem":  vaultValueString,
		"server_name":     vaultValueString,
	}

	// Keys every auth map may carry, whichever method it selects.
	vaultStrictAuthCommonKeys = map[string]vaultValueKind{
		"method":      vaultValueString,
		"type":        vaultValueString,
		"mount_point": vaultValueString,
		"mount":       vaultValueString,
	}

	// Keys each method understands, on top of the common set.
	vaultStrictAuthMethodKeys = map[string]map[string]vaultValueKind{
		"token": {"token": vaultValueString},
		"approle": {
			"role_id": vaultValueString, "secret_id": vaultValueString,
			"secret_id_file": vaultValueString, "secret_id_env": vaultValueString,
			"wrapping_token": vaultValueBool,
		},
		"ldap": {"username": vaultValueString, "password": vaultValueString},
		"jwt":  {"role": vaultValueString, "jwt": vaultValueString},
		"oidc": {
			"role": vaultValueString, "redirect_uri": vaultValueString,
			"callback_timeout_seconds": vaultValueInt,
		},
		"gcp": {
			"role": vaultValueString, "auth_type": vaultValueString,
			"service_account_email": vaultValueString,
		},
		"gcp_jwt": {"role": vaultValueString, "jwt": vaultValueString},
		"azure":   {"role": vaultValueString, "resource": vaultValueString},
		"azure_jwt": {
			"role": vaultValueString, "jwt": vaultValueString,
			"vm_name": vaultValueString, "vmss_name": vaultValueString,
			"subscription_id": vaultValueString, "resource_group_name": vaultValueString,
		},
		"aws": {
			"role": vaultValueString, "iam_server_id_header": vaultValueString,
			"region": vaultValueString,
		},
		"aws_signed_request": {
			"role": vaultValueString, "iam_server_id_header": vaultValueString,
			"iam_http_request_method": vaultValueString, "iam_request_url": vaultValueString,
			"iam_request_body": vaultValueString, "iam_request_headers": vaultValueString,
		},
		"kubernetes": {
			"role": vaultValueString, "jwt": vaultValueString,
			"token_path": vaultValueString, "token_env": vaultValueString,
		},
	}

	// Method aliases share a key set with their canonical name.
	vaultStrictAuthAliases = map[string]string{
		"k8s": "kubernetes", "aws_iam": "aws", "awsiam": "aws",
	}

	// vaultAuthContracts is the complete declarative contract for each method:
	// what must be declared for the method to run at all.
	//
	// Every rule here already exists inside the auth implementations —
	// TokenAuth, AppRoleAuth, LDAPAuth, JWTAuth, KubernetesAuth, AzureAuth,
	// GCPAuth, AzureJWTAuth, AWSIAMSignedRequestAuth all reject the same
	// omissions. The difference is when. Enforced only there, an incomplete
	// declaration builds a provider, opens a connection, and fails during
	// authentication; enforced here it fails at admission, before anything is
	// contacted, which is what strict mode promises.
	//
	// This is data rather than a conditional per method so that a rule cannot
	// be half-implemented: requiring exactly one AppRole secret-id source while
	// not requiring role_id enforced half of one contract and read as complete.
	vaultAuthContracts = map[string]vaultAuthContract{
		"token": {
			oneOf: []vaultSourceGroup{{
				what:     "a token",
				keys:     []string{"token"},
				rootKeys: []string{"token"},
				required: true,
			}},
		},
		"approle": {
			required: []string{"role_id"},
			oneOf: []vaultSourceGroup{{
				what:     "an AppRole secret id",
				keys:     []string{"secret_id", "secret_id_file", "secret_id_env"},
				required: true,
			}},
		},
		// PasswordProvider is not reachable from a declaration, so the
		// password must be declared.
		"ldap":      {required: []string{"username", "password"}},
		"jwt":       {required: []string{"role", "jwt"}},
		"gcp_jwt":   {required: []string{"role", "jwt"}},
		"azure":     {required: []string{"role"}},
		"azure_jwt": {required: []string{"role", "jwt"}},
		"kubernetes": {
			required: []string{"role"},
			oneOf: []vaultSourceGroup{{
				what: "a Kubernetes service-account token",
				keys: []string{"jwt", "token_path", "token_env"},
				// Not required: with none declared the in-cluster default path
				// applies, which is a documented default rather than a silence.
				required: false,
			}},
		},
		"gcp": {
			required: []string{"role"},
			enums:    map[string][]string{"auth_type": {"", "gce", "iam"}},
			conditional: []vaultConditionalRequirement{{
				when: "auth_type", equals: []string{"iam"},
				requires: []string{"service_account_email"},
			}},
		},
		"aws_signed_request": {
			required: []string{
				"iam_http_request_method", "iam_request_url",
				"iam_request_body", "iam_request_headers",
			},
		},
		// aws discovers credentials from the environment and may infer the
		// role from the IAM principal, so nothing is mandatory.
		"aws": {},
		// oidc's role is optional: Vault falls back to the mount's default
		// role. callback_timeout_seconds is constrained by positiveInts.
		"oidc": {positiveInts: []string{"callback_timeout_seconds"}},
	}
)

// vaultAuthContract is what a method needs before it can run.
type vaultAuthContract struct {
	// required names fields that must each be declared.
	required []string
	// oneOf names groups of alternative routes to a single value.
	oneOf []vaultSourceGroup
	// enums restricts a field to a set of values; "" permits omission.
	enums map[string][]string
	// conditional names requirements that apply only for certain values.
	conditional []vaultConditionalRequirement
	// positiveInts names integer fields whose zero is not a value.
	positiveInts []string
}

// vaultConditionalRequirement is a requirement that applies only when another
// field holds one of a set of values, such as GCP IAM needing a service
// account email while GCE mode does not.
type vaultConditionalRequirement struct {
	when     string
	equals   []string
	requires []string
}

// vaultSourceGroup is a set of keys that are alternative routes to one value.
// rootKeys names spellings that live in the root configuration rather than the
// auth map, which is how a root token satisfies token authentication.
type vaultSourceGroup struct {
	what     string
	keys     []string
	rootKeys []string
	required bool
}

// admitStrictVaultConfig rejects anything the provider does not understand,
// at every level, and rejects values that are recognised but unusable.
func admitStrictVaultConfig(cfg map[string]any) error {
	if err := rejectUnknownKeys("", cfg, vaultStrictRootKeys); err != nil {
		return err
	}
	if err := rejectConflictingAliases(cfg,
		[2]string{"address", "url"}, [2]string{"mount_point", "mount"}); err != nil {
		return err
	}
	if err := admitStrictVaultVerify(cfg); err != nil {
		return err
	}
	if tls, ok := cfg["tls"]; ok {
		nested, isMap := tls.(map[string]any)
		if !isMap {
			return fmt.Errorf("%w: tls must be a map", ErrVaultStrictConfiguration)
		}
		if err := rejectUnknownKeys("tls", nested, vaultStrictTLSKeys); err != nil {
			return err
		}
	}
	method, err := admitStrictVaultAuth(cfg)
	if err != nil {
		return err
	}
	return admitStrictVaultSemantics(cfg, method)
}

// admitStrictVaultAuth checks the auth declaration and returns the canonical
// method it selects, or "" when none is declared.
func admitStrictVaultAuth(cfg map[string]any) (string, error) {
	raw, ok := cfg["auth"]
	if !ok {
		return "", nil
	}
	nested, isMap := raw.(map[string]any)
	if !isMap {
		// A bare method name. It carries no keys to check, but it is still a
		// declaration and it still selects a method: leaving it unvalidated
		// meant auth: "no-such-method" failed later inside construction, with
		// an error that did not wrap the strict sentinel a caller matches on.
		bare, isString := raw.(string)
		if !isString {
			return "", fmt.Errorf("%w: auth must be a method name or a map",
				ErrVaultStrictConfiguration)
		}
		return canonicalVaultAuthMethod(bare)
	}
	// The keys that select the method are validated before they are used.
	// Without this a method: 42 reached selfString, which answers a non-string
	// with "", and the failure surfaced as an unsupported method named "" —
	// true, but not what went wrong. It also puts the alias comparison below
	// on values already known to be usable.
	for _, key := range []string{"method", "type", "mount", "mount_point"} {
		value, present := nested[key]
		if !present {
			continue
		}
		if err := admitVaultValue("auth", key, value, vaultStrictAuthCommonKeys[key]); err != nil {
			return "", err
		}
	}
	// method/type and mount/mount_point are two spellings each. Selection is
	// first-match, so accepting both means one is silently discarded and which
	// one is an implementation detail the author cannot see. The root-level
	// check does not reach here: this map is a separate scope with its own
	// aliases, and leaving it unchecked is how auth {method: token, type: ldap}
	// came to be accepted with the type ignored.
	if err := rejectConflictingAliases(nested,
		[2]string{"method", "type"}, [2]string{"mount_point", "mount"}); err != nil {
		return "", err
	}
	method, err := canonicalVaultAuthMethod(selfString(nested, "method", "type"))
	if err != nil {
		return "", err
	}
	methodKeys := vaultStrictAuthMethodKeys[method]
	allowed := make(map[string]vaultValueKind, len(methodKeys)+len(vaultStrictAuthCommonKeys))
	for key, kind := range vaultStrictAuthCommonKeys {
		allowed[key] = kind
	}
	for key, kind := range methodKeys {
		allowed[key] = kind
	}
	return method, rejectUnknownKeys("auth", nested, allowed)
}

// canonicalVaultAuthMethod normalizes a declared method name and rejects one
// the provider cannot run. Case and surrounding space are not meaningful;
// aliases resolve to the name whose key set and constructor they share.
func canonicalVaultAuthMethod(declared string) (string, error) {
	method := strings.ToLower(strings.TrimSpace(declared))
	if canonical, aliased := vaultStrictAuthAliases[method]; aliased {
		method = canonical
	}
	if _, known := vaultStrictAuthMethodKeys[method]; !known {
		return "", fmt.Errorf("%w: auth method %q is not supported",
			ErrVaultStrictConfiguration, method)
	}
	return method, nil
}

// rejectUnknownKeys refuses names the scope does not understand, then refuses
// values the scope cannot use. Both are the same promise: a strict declaration
// either takes effect or is reported, never accepted and dropped.
func rejectUnknownKeys(scope string, cfg map[string]any, allowed map[string]vaultValueKind) error {
	unknown := make([]string, 0, len(cfg))
	for key := range cfg {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return admitVaultValues(scope, cfg, allowed)
	}
	sort.Strings(unknown)
	where := "unrecognized setting"
	if scope != "" {
		where = "unrecognized " + scope + " setting"
	}
	return fmt.Errorf("%w: %s %s", ErrVaultStrictConfiguration, where, strings.Join(unknown, ", "))
}

// rejectConflictingAliases refuses two spellings of one setting. Accepting both
// means one of them is silently ignored, and which one is an implementation
// detail the author cannot see.
func rejectConflictingAliases(cfg map[string]any, pairs ...[2]string) error {
	for _, pair := range pairs {
		first, hasFirst := cfg[pair[0]]
		second, hasSecond := cfg[pair[1]]
		if !hasFirst || !hasSecond {
			continue
		}
		// Compare the values, not their printed forms. fmt.Sprint renders true
		// and "true" identically, so address: true beside url: "true" read as
		// agreeing spellings when one of them is not even a usable address.
		// Every alias pair here is string-valued, so anything else disagrees by
		// construction and is caught by value admission besides.
		firstText, firstIsString := first.(string)
		secondText, secondIsString := second.(string)
		if firstIsString && secondIsString &&
			strings.TrimSpace(firstText) == strings.TrimSpace(secondText) {
			continue
		}
		return fmt.Errorf("%w: %s and %s are the same setting and disagree",
			ErrVaultStrictConfiguration, pair[0], pair[1])
	}
	return nil
}

// admitStrictVaultSemantics enforces what a declaration means, having already
// established what shape it has.
//
// Closing the schema over names caught a misspelling and closing it over kinds
// caught a wrong type, but a correctly spelled setting of the right type could
// still be discarded: a root token beside approle authentication went to a
// constructor that never reads it, and an OIDC callback timeout of 0 became the
// default. The invariant is one thing, so it is enforced in one place — every
// present declaration is consumed as declared, refused as conflicting, or
// covered by a documented default.
func admitStrictVaultSemantics(cfg map[string]any, method string) error {
	if err := admitVaultIntDomain(cfg, "kv_version", 1, 2); err != nil {
		return err
	}
	if err := admitVaultIntDomain(cfg, "retry_limit", 0, math.MaxInt); err != nil {
		return err
	}
	if err := admitStrictVaultToken(cfg, method); err != nil {
		return err
	}
	if method == "" {
		return nil
	}
	auth, _ := cfg["auth"].(map[string]any)
	return admitVaultAuthContract(cfg, auth, method, vaultAuthContracts[method])
}

// admitVaultAuthContract enforces one method's declarative contract.
func admitVaultAuthContract(root, auth map[string]any, method string, contract vaultAuthContract) error {
	declared := func(in map[string]any, key string) bool {
		value, present := in[key]
		if !present {
			return false
		}
		// Value admission has already refused a wrong type or an empty
		// string, so presence here means a usable value.
		_ = value
		return true
	}

	missing := make([]string, 0, len(contract.required))
	for _, key := range contract.required {
		if !declared(auth, key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: %s auth requires %s",
			ErrVaultStrictConfiguration, method, strings.Join(missing, ", "))
	}

	for _, group := range contract.oneOf {
		if err := admitVaultSourceGroup(method, root, auth, group); err != nil {
			return err
		}
	}

	for key, allowed := range contract.enums {
		value := strings.ToLower(strings.TrimSpace(selfString(auth, key)))
		permitted := false
		for _, candidate := range allowed {
			if value == candidate {
				permitted = true
				break
			}
		}
		if !permitted {
			named := make([]string, 0, len(allowed))
			for _, candidate := range allowed {
				if candidate != "" {
					named = append(named, candidate)
				}
			}
			return fmt.Errorf("%w: %s auth %s must be one of %s",
				ErrVaultStrictConfiguration, method, key, strings.Join(named, ", "))
		}
	}

	for _, rule := range contract.conditional {
		value := strings.ToLower(strings.TrimSpace(selfString(auth, rule.when)))
		applies := false
		for _, candidate := range rule.equals {
			if value == candidate {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}
		for _, key := range rule.requires {
			if !declared(auth, key) {
				return fmt.Errorf("%w: %s auth with %s %q requires %s",
					ErrVaultStrictConfiguration, method, rule.when, value, key)
			}
		}
	}

	for _, key := range contract.positiveInts {
		if _, present := auth[key]; !present {
			continue
		}
		if err := admitVaultIntDomain(auth, key, 1, math.MaxInt); err != nil {
			return err
		}
	}
	return nil
}

// admitVaultIntDomain bounds a recognized integer setting. The value has already
// been established as a whole number, so only its range is in question here.
func admitVaultIntDomain(cfg map[string]any, key string, low, high int) error {
	if _, present := cfg[key]; !present {
		return nil
	}
	value, err := selfInt(cfg, key, low)
	if err != nil {
		return fmt.Errorf("%w: %s must be a whole number", ErrVaultStrictConfiguration, key)
	}
	if value < low || value > high {
		if high == math.MaxInt {
			return fmt.Errorf("%w: %s must be at least %d",
				ErrVaultStrictConfiguration, key, low)
		}
		return fmt.Errorf("%w: %s must be between %d and %d",
			ErrVaultStrictConfiguration, key, low, high)
	}
	return nil
}

// admitStrictVaultToken decides what a root token means beside an auth block.
//
// Construction hands the root token to whichever method was selected, and only
// token authentication reads it. Declaring one beside approle therefore
// configured nothing. Declaring one beside a nested auth.token was worse: both
// name the same credential, the root one won, and the nested one vanished.
func admitStrictVaultToken(cfg map[string]any, method string) error {
	root, hasRoot := cfg["token"]
	if !hasRoot {
		return nil
	}
	// No auth block at all: the root token is the whole declaration and
	// construction selects token authentication from it.
	if method == "" {
		return nil
	}
	if method != "token" {
		return fmt.Errorf(
			"%w: token is only read by token authentication, but auth selects %q; "+
				"move it to auth.token or remove it",
			ErrVaultStrictConfiguration, method)
	}
	auth, _ := cfg["auth"].(map[string]any)
	nested, hasNested := auth["token"]
	if !hasNested {
		return nil
	}
	// Both name one credential. Identical spellings are redundant; different
	// ones mean one is silently discarded. Neither value is named in the error.
	if fmt.Sprint(root) != fmt.Sprint(nested) {
		return fmt.Errorf(
			"%w: token and auth.token are the same credential and disagree",
			ErrVaultStrictConfiguration)
	}
	return nil
}

// admitVaultSourceGroup enforces how many of a group of alternative routes to
// one credential may be declared. Naming two means construction reads one and
// drops the other without saying which.
func admitVaultSourceGroup(method string, root, auth map[string]any, group vaultSourceGroup) error {
	declared := make([]string, 0, len(group.keys)+len(group.rootKeys))
	for _, key := range group.keys {
		if _, present := auth[key]; present {
			declared = append(declared, "auth."+key)
		}
	}
	for _, key := range group.rootKeys {
		if _, present := root[key]; present {
			declared = append(declared, key)
		}
	}
	// A root spelling and its auth spelling that agree are one declaration,
	// not two; admitStrictVaultToken has already refused them when they differ.
	if len(declared) == 2 && len(group.rootKeys) == 1 && len(group.keys) == 1 &&
		declared[0] == "auth."+group.keys[0] && declared[1] == group.rootKeys[0] {
		declared = declared[:1]
	}
	sort.Strings(declared)
	switch {
	case len(declared) > 1:
		return fmt.Errorf("%w: %s auth declares %s for %s; exactly one is read",
			ErrVaultStrictConfiguration, method, strings.Join(declared, " and "), group.what)
	case len(declared) == 0 && group.required:
		return fmt.Errorf("%w: %s auth requires %s: declare one of %s",
			ErrVaultStrictConfiguration, method, group.what, strings.Join(group.keys, ", "))
	}
	return nil
}

// admitVaultValues checks every present setting against its declared kind, in a
// stable order so the reported failure does not depend on map iteration.
func admitVaultValues(scope string, cfg map[string]any, allowed map[string]vaultValueKind) error {
	keys := make([]string, 0, len(cfg))
	for key := range cfg {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := admitVaultValue(scope, key, cfg[key], allowed[key]); err != nil {
			return err
		}
	}
	return nil
}

// admitVaultValue reports whether value is usable as kind.
//
// The bool, integer, and duration cases delegate to the very readers
// construction uses, so admission cannot come to accept something construction
// then rejects, or the reverse. The error names the setting and its expected
// shape but never the value, which may be a credential.
func admitVaultValue(scope, key string, value any, kind vaultValueKind) error {
	fail := func(want string) error {
		where := key
		if scope != "" {
			where = scope + "." + key
		}
		return fmt.Errorf("%w: %s must be %s", ErrVaultStrictConfiguration, where, want)
	}
	switch kind {
	case vaultValueString:
		text, isString := value.(string)
		if !isString {
			return fail("a string")
		}
		// An empty declaration is indistinguishable from omission once read,
		// yet it looks deliberate in the file. Strict mode says which it is.
		if strings.TrimSpace(text) == "" {
			return fail("a non-empty string")
		}
	case vaultValueBool:
		if _, err := selfBool(map[string]any{key: value}, key, false); err != nil {
			return fail("a boolean")
		}
	case vaultValueInt:
		if _, err := selfInt(map[string]any{key: value}, key, 0); err != nil {
			return fail("a whole number")
		}
	case vaultValueDuration:
		text, isString := value.(string)
		if !isString || strings.TrimSpace(text) == "" {
			return fail("a duration string such as \"30s\"")
		}
		if _, err := time.ParseDuration(strings.TrimSpace(text)); err != nil {
			return fail("a valid duration such as \"30s\"")
		}
	case vaultValueMap:
		if _, isMap := value.(map[string]any); !isMap {
			return fail("a map")
		}
	case vaultValueMethodOrMap:
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				return fail("a method name or a map")
			}
		case map[string]any:
		default:
			return fail("a method name or a map")
		}
	}
	return nil
}

// admitStrictVaultVerify refuses verify: false.
//
// Strict mode builds the client hermetically, and a hermetic client cannot have
// certificate verification disabled by any option or variable — WithVaultVerify
// is ignored there. Accepting the setting anyway would tell an author their
// declaration was honored when the opposite of what they asked for is what
// happens. Omission and an explicit true both mean "verify", which is what the
// transport does, so only false is refused.
func admitStrictVaultVerify(cfg map[string]any) error {
	verify, err := selfBool(cfg, "verify", true)
	if err != nil {
		return err
	}
	if !verify {
		return fmt.Errorf(
			"%w: verify must not be false; strict mode builds the transport "+
				"hermetically and cannot disable certificate verification",
			ErrVaultStrictConfiguration)
	}
	return nil
}

// validateVaultTimeout rejects a timeout that cannot mean what it says. A
// negative duration is not a shorter deadline, and zero is not "no timeout"
// here: it is indistinguishable from the field being absent, so a caller who
// wrote it would get the default without being told.
func validateVaultTimeout(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%w: timeout must be greater than zero, got %s",
			ErrVaultStrictConfiguration, d)
	}
	return nil
}

// validateVaultProxy rejects a proxy address that cannot be dialled. A relative
// or scheme-less URL silently becomes no proxy at all, which is the opposite of
// what the author asked for.
//
// The error names the scheme and host only. A proxy URL may carry credentials
// in its user information, and echoing the parsed URL would put them in a log.
func validateVaultProxy(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: proxy is not a valid URL", ErrVaultStrictConfiguration)
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	case "":
		return fmt.Errorf("%w: proxy must be absolute and carry a scheme",
			ErrVaultStrictConfiguration)
	default:
		return fmt.Errorf("%w: proxy scheme %q is not supported",
			ErrVaultStrictConfiguration, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: proxy must name a host", ErrVaultStrictConfiguration)
	}
	return nil
}
