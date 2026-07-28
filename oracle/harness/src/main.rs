//! Differential-oracle harness.
//!
//! Reads a corpus file (see oracle/SCHEMA.md), evaluates every row against
//! BAML's pinned Rust minijinja engine fork, and writes one JSON document to
//! stdout describing the exact outcome of each row: the exact rendered bytes,
//! the normalized boolean, or the error category.
//!
//! This is a *diagnostic microscope on the engine*, not the whole-stack BAML
//! oracle. It links the exact engine revision BAML builds against with BAML's
//! exact cargo feature set, but it deliberately does NOT reconstruct BAML's
//! environment (get_env, pycompat, regex_match/sum, prompt lowering). Corpus
//! rows therefore declare an engine profile: `stock` is stock engine defaults,
//! and `pycompat` adds the generic unknown-method callback from
//! minijinja-contrib — an installable module, not BAML's environment. When the
//! BAML profile lands it becomes a third profile here and in the Go runner,
//! and the schema does not change.
//!
//! Usage:  mj-oracle-harness <corpus-dir>   # JSON document on stdout

use std::cmp::Ordering;
use std::fmt;
use std::panic::{self, AssertUnwindSafe};
use std::sync::Arc;

use minijinja::value::{DynObject, Object, ObjectRepr, Value};
use minijinja::{Environment, ErrorKind};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

const SCHEMA_VERSION: u32 = 1;
const HARNESS_VERSION: &str = "1";
const ENGINE_REPO: &str = "https://github.com/boundaryml/minijinja";
const ENGINE_BRANCH: &str = "value-cmp";
const ENGINE_REV: &str = "8cfc770a5dffeda2de5b910d2b9f870d7edeff7c";
const ENGINE_FEATURES: &[&str] = &[
    "macros",
    "builtins",
    "debug",
    "preserve_order",
    "adjacent_loop_items",
    "unicode",
    "json",
    "unstable_machinery",
    "custom_syntax",
    "deserialization",
    "serde",
];

// ---------------------------------------------------------------------------
// Corpus schema (must stay in lockstep with oracle/corpus.go and SCHEMA.md)
// ---------------------------------------------------------------------------

#[derive(Deserialize)]
struct Corpus {
    schema_version: u32,
    rows: Vec<Row>,
}

#[derive(Deserialize)]
struct Row {
    id: String,
    #[serde(default)]
    #[allow(dead_code)]
    surface: String,
    form: Form,
    source: String,
    #[serde(default)]
    profile: Profile,
    #[serde(default)]
    inputs: Vec<Binding>,
    #[serde(default)]
    expect: Expect,
    #[serde(default)]
    #[allow(dead_code)]
    notes: String,
}

#[derive(Deserialize, PartialEq)]
#[serde(rename_all = "snake_case")]
enum Form {
    /// A bare expression. Wrapped as `{{ expr }}`, the same shape BAML uses to
    /// evaluate a constraint predicate (jinja_helpers.rs:67-94).
    Expression,
    /// A full template, used verbatim.
    Template,
}

/// Engine configuration a row is evaluated under.
///
/// Every variant is *engine configuration only* — the same knobs the Go runner
/// sets on its side, plus the generic unknown-method module BAML installs.
/// BAML's own environment (get_env, regex_match/sum, prompt lowering,
/// ctx/_/enum globals) is deliberately not here; it arrives as its own profile
/// in a later slice.
///
/// The whitespace variants exist because trim_blocks/lstrip_blocks/
/// keep_trailing_newline cannot be reached from template source. BAML's own
/// environment sets trim_blocks and lstrip_blocks (jinja_helpers.rs:7-35), so
/// the machinery they drive has to be compared under them too.
#[derive(Deserialize, Default, Clone, Copy, PartialEq)]
#[serde(rename_all = "snake_case")]
enum Profile {
    /// Stock engine defaults. No BAML environment setup.
    #[default]
    Stock,
    /// set_trim_blocks(true)
    TrimBlocks,
    /// set_lstrip_blocks(true)
    LstripBlocks,
    /// Both, the combination BAML's own environment uses.
    TrimLstrip,
    /// set_keep_trailing_newline(true)
    KeepTrailingNewline,
    /// Stock engine defaults plus the Python-compatible unknown-method
    /// callback from `minijinja-contrib` — the one BAML installs
    /// (jinja_helpers.rs:34). It is a *generic* engine capability driven by an
    /// installable module, not BAML's environment: no regex_match, no sum, no
    /// none-formatter. Those belong to the BAML profile, a later slice.
    Pycompat,
}

impl Profile {
    fn apply(&self, env: &mut Environment) {
        match self {
            Profile::Stock => {}
            Profile::TrimBlocks => env.set_trim_blocks(true),
            Profile::LstripBlocks => env.set_lstrip_blocks(true),
            Profile::TrimLstrip => {
                env.set_trim_blocks(true);
                env.set_lstrip_blocks(true);
            }
            Profile::KeepTrailingNewline => env.set_keep_trailing_newline(true),
            Profile::Pycompat => {
                env.set_unknown_method_callback(minijinja_contrib::pycompat::unknown_method_callback)
            }
        }
    }
}

#[derive(Deserialize, Default, PartialEq)]
#[serde(rename_all = "snake_case")]
enum Expect {
    /// Compare exact rendered bytes.
    #[default]
    Bytes,
    /// Compare the normalized boolean as well as the bytes.
    Boolean,
    /// The row is expected to fail; compare error categories.
    Error,
}

#[derive(Deserialize)]
struct Binding {
    name: String,
    value: TypedValue,
}

#[derive(Deserialize)]
struct MapEntry {
    key: String,
    value: TypedValue,
}

/// Explicitly typed corpus inputs. Types are tagged rather than inferred from
/// JSON so that int/float and map ordering survive the trip through both
/// runtimes unambiguously.
#[derive(Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
enum TypedValue {
    Int { value: i64 },
    Float { value: f64 },
    Bool { value: bool },
    String { value: String },
    Null,
    List { items: Vec<TypedValue> },
    /// Entries are ordered; the order is part of the fixture.
    Map { entries: Vec<MapEntry> },
    /// A generic host object that answers the engine's `value_cmp` hook by its
    /// canonical value while displaying something else. This is the generic
    /// shape of BAML's enum object (alias display, canonical-value identity)
    /// with no BAML types involved.
    CmpObject { canonical: String, display: String },
}

// ---------------------------------------------------------------------------
// The value_cmp test object
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
struct CmpObject {
    canonical: String,
    display: String,
}

impl Object for CmpObject {
    fn repr(self: &Arc<Self>) -> ObjectRepr {
        ObjectRepr::Plain
    }

    /// The hook that is BoundaryML's sole delta over upstream minijinja: the
    /// engine asks the object to compare itself against an arbitrary Value
    /// before falling back to ordinary equality/ordering.
    fn value_cmp(self: &Arc<Self>, other: &Value) -> Option<Ordering> {
        if let Some(other_str) = other.as_str() {
            return Some(self.canonical.as_str().cmp(other_str));
        }
        if let Some(other_obj) = other.as_object() {
            return self.custom_cmp(other_obj);
        }
        None
    }

    fn custom_cmp(self: &Arc<Self>, other: &DynObject) -> Option<Ordering> {
        let other = other.downcast_ref::<Self>()?;
        Some(self.canonical.cmp(&other.canonical))
    }

    fn render(self: &Arc<Self>, f: &mut fmt::Formatter<'_>) -> fmt::Result
    where
        Self: Sized + 'static,
    {
        write!(f, "{}", self.display)
    }
}

fn build_value(tv: &TypedValue) -> Value {
    match tv {
        TypedValue::Int { value } => Value::from(*value),
        TypedValue::Float { value } => Value::from(*value),
        TypedValue::Bool { value } => Value::from(*value),
        TypedValue::String { value } => Value::from(value.as_str()),
        TypedValue::Null => Value::from(()),
        TypedValue::List { items } => Value::from(items.iter().map(build_value).collect::<Vec<_>>()),
        TypedValue::Map { entries } => Value::from_iter(
            entries
                .iter()
                .map(|e| (Value::from(e.key.as_str()), build_value(&e.value))),
        ),
        TypedValue::CmpObject { canonical, display } => Value::from_object(CmpObject {
            canonical: canonical.clone(),
            display: display.clone(),
        }),
    }
}

// ---------------------------------------------------------------------------
// Outcome schema (must stay in lockstep with oracle/outcome.go)
// ---------------------------------------------------------------------------

#[derive(Serialize)]
#[serde(tag = "status", rename_all = "snake_case")]
enum Outcome {
    Ok {
        render: String,
        boolean: Option<bool>,
    },
    Error {
        category: String,
        kind: String,
        message: String,
    },
    Panic {
        message: String,
    },
    /// The harness could not model the row at all. Never a silent pass: the Go
    /// runner labels any mismatch involving this as `harness-incomplete`.
    ///
    /// Nothing constructs it here today — an unknown profile now fails corpus
    /// deserialization outright, which is louder than a per-row outcome — but
    /// it stays part of the shared schema because the Go runner still emits it
    /// and both sides must be able to parse it.
    #[allow(dead_code)]
    Unsupported {
        message: String,
    },
}

#[derive(Serialize)]
struct ResultRow {
    id: String,
    outcome: Outcome,
}

#[derive(Serialize)]
struct Provenance {
    engine_repo: &'static str,
    engine_branch: &'static str,
    engine_rev: &'static str,
    engine_features: &'static [&'static str],
    harness_version: &'static str,
    os: &'static str,
    arch: &'static str,
    corpus_sha256: String,
}

#[derive(Serialize)]
struct Output {
    schema_version: u32,
    provenance: Provenance,
    results: Vec<ResultRow>,
}

/// Canonical error vocabulary shared with the Go runner. Display strings differ
/// between the two implementations, so categories — not messages — are what the
/// differential compares.
fn error_category(kind: ErrorKind) -> &'static str {
    match kind {
        ErrorKind::NonPrimitive => "non_primitive",
        ErrorKind::NonKey => "non_key",
        ErrorKind::InvalidOperation => "invalid_operation",
        ErrorKind::SyntaxError => "syntax",
        ErrorKind::TemplateNotFound => "template_not_found",
        ErrorKind::TooManyArguments => "too_many_arguments",
        ErrorKind::MissingArgument => "missing_argument",
        ErrorKind::UnknownFilter => "unknown_filter",
        ErrorKind::UnknownTest => "unknown_test",
        ErrorKind::UnknownFunction => "unknown_function",
        ErrorKind::UnknownMethod => "unknown_method",
        ErrorKind::BadEscape => "bad_escape",
        ErrorKind::UndefinedError => "undefined",
        ErrorKind::BadSerialization => "bad_serialization",
        ErrorKind::CannotUnpack => "cannot_unpack",
        ErrorKind::WriteFailure => "write_failure",
        _ => "other",
    }
}

fn normalize_boolean(rendered: &str) -> Option<bool> {
    match rendered.trim() {
        "true" | "True" => Some(true),
        "false" | "False" => Some(false),
        _ => None,
    }
}

fn evaluate(row: &Row) -> Outcome {
    let source = match row.form {
        // Same wrapping BAML uses for constraint predicates.
        Form::Expression => format!("{{{{ {} }}}}", row.source),
        Form::Template => row.source.clone(),
    };

    let mut env = Environment::new();
    // Whitespace configuration must be in place before the template is added:
    // trailing-newline stripping happens when the tokenizer is constructed.
    row.profile.apply(&mut env);
    // `.txt` keeps the default auto-escape callback on "none" on both sides.
    let name = "corpus.txt";
    // `add_template` borrows for `'source`; `add_template_owned` is behind the
    // `loader` feature, which BAML deliberately does not enable. One leaked
    // string per row is the honest way to keep the feature set BAML-exact.
    let source: &'static str = Box::leak(source.into_boxed_str());
    if let Err(err) = env.add_template(name, source) {
        return Outcome::Error {
            category: error_category(err.kind()).into(),
            kind: format!("{:?}", err.kind()),
            message: err.to_string(),
        };
    }
    let tmpl = match env.get_template(name) {
        Ok(t) => t,
        Err(err) => {
            return Outcome::Error {
                category: error_category(err.kind()).into(),
                kind: format!("{:?}", err.kind()),
                message: err.to_string(),
            }
        }
    };

    let ctx = Value::from_iter(
        row.inputs
            .iter()
            .map(|b| (Value::from(b.name.as_str()), build_value(&b.value))),
    );

    match tmpl.render(ctx) {
        Ok(rendered) => {
            let boolean = if row.expect == Expect::Boolean {
                normalize_boolean(&rendered)
            } else {
                None
            };
            Outcome::Ok {
                render: rendered,
                boolean,
            }
        }
        Err(err) => Outcome::Error {
            category: error_category(err.kind()).into(),
            kind: format!("{:?}", err.kind()),
            message: err.to_string(),
        },
    }
}

/// Reads the corpus as an ordered list of (name, bytes) plus the concatenation
/// the digest is taken over. A single file is still accepted so the harness can
/// be pointed at one fixture by hand.
fn read_corpus_bytes(path: &str) -> Result<(Vec<u8>, Vec<(String, Vec<u8>)>), std::io::Error> {
    let meta = std::fs::metadata(path)?;
    if !meta.is_dir() {
        let bytes = std::fs::read(path)?;
        return Ok((bytes.clone(), vec![(path.to_string(), bytes)]));
    }
    let mut names: Vec<_> = std::fs::read_dir(path)?
        .collect::<Result<Vec<_>, _>>()?
        .into_iter()
        .map(|e| e.path())
        .filter(|p| p.extension().map(|e| e == "json").unwrap_or(false))
        .collect();
    names.sort();
    let mut all = Vec::new();
    let mut files = Vec::new();
    for p in names {
        let bytes = std::fs::read(&p)?;
        all.extend_from_slice(&bytes);
        files.push((p.display().to_string(), bytes));
    }
    Ok((all, files))
}

fn main() {
    let path = match std::env::args().nth(1) {
        Some(p) => p,
        None => {
            eprintln!("usage: mj-oracle-harness <corpus-dir-or-file>");
            std::process::exit(2);
        }
    };
    // The corpus is a directory of files, read in sorted order, so that
    // parallel workstreams add rows without contending for one file. The
    // digest is taken over the concatenated bytes in that same order, which is
    // what ties a recording to the exact corpus it was produced from.
    let (raw, files) = match read_corpus_bytes(&path) {
        Ok(rv) => rv,
        Err(err) => {
            eprintln!("cannot read corpus {path}: {err}");
            std::process::exit(2);
        }
    };
    let mut rows: Vec<Row> = Vec::new();
    for (name, bytes) in &files {
        let corpus: Corpus = match serde_json::from_slice(bytes) {
            Ok(c) => c,
            Err(err) => {
                eprintln!("cannot parse {name}: {err}");
                std::process::exit(2);
            }
        };
        if corpus.schema_version != SCHEMA_VERSION {
            eprintln!(
                "{name}: corpus schema_version {} != harness schema_version {}",
                corpus.schema_version, SCHEMA_VERSION
            );
            std::process::exit(2);
        }
        rows.extend(corpus.rows);
    }
    let corpus = Corpus {
        schema_version: SCHEMA_VERSION,
        rows,
    };

    let corpus_sha256 = format!("{:x}", Sha256::digest(&raw));

    // A panicking row is an outcome, not a harness crash.
    panic::set_hook(Box::new(|_| {}));

    let results = corpus
        .rows
        .iter()
        .map(|row| {
            let outcome = match panic::catch_unwind(AssertUnwindSafe(|| evaluate(row))) {
                Ok(o) => o,
                Err(payload) => {
                    let message = payload
                        .downcast_ref::<&str>()
                        .map(|s| (*s).to_string())
                        .or_else(|| payload.downcast_ref::<String>().cloned())
                        .unwrap_or_else(|| "panic".into());
                    Outcome::Panic { message }
                }
            };
            ResultRow {
                id: row.id.clone(),
                outcome,
            }
        })
        .collect();
    let _ = panic::take_hook();

    let out = Output {
        schema_version: SCHEMA_VERSION,
        provenance: Provenance {
            engine_repo: ENGINE_REPO,
            engine_branch: ENGINE_BRANCH,
            engine_rev: ENGINE_REV,
            engine_features: ENGINE_FEATURES,
            harness_version: HARNESS_VERSION,
            os: std::env::consts::OS,
            arch: std::env::consts::ARCH,
            corpus_sha256,
        },
        results,
    };

    match serde_json::to_string_pretty(&out) {
        Ok(s) => println!("{s}"),
        Err(err) => {
            eprintln!("cannot serialize output: {err}");
            std::process::exit(2);
        }
    }
}
