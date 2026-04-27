// Package mremoteng implements the mRemoteNG XML (Confcons.xml) and CSV
// importer for goremote.
//
// The importer is intentionally tolerant: unknown protocols, unknown
// attributes, and unsupported inheritance flags are preserved on the
// resulting domain nodes (under Settings keys like "legacy_protocol",
// "legacy_attr_*", "legacy_inherit_*") and surfaced as warnings on the
// Result. Silent data loss is never acceptable here — per requirements.md
// §4.5 we prefer explicit warnings over dropped data.
//
// The mRemoteNG on-disk format supports AES-GCM encrypted passwords. This
// importer never attempts to decrypt them: if a non-empty Password attribute
// is present it is captured as "legacy_password_blob" and the user is
// warned to re-enter credentials or wire up a credential provider.
package mremoteng
