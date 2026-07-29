//! Unicode table generator.
//!
//! The engine's string builtins are Rust `str`/`char` operations: `|upper` is
//! `str::to_uppercase`, `.isalpha()` is `char::is_alphabetic`, and so on. Go's
//! `strings`/`unicode` package answers several of those differently — simple
//! rather than full case mapping, `Ll` rather than the `Lowercase` property —
//! so the fork cannot reach byte-exactness by calling the Go stdlib.
//!
//! This dumps the Rust side of every table the fork needs, for every Unicode
//! scalar, so the Go tables are *derived from the reference implementation*
//! rather than reconstructed from a spec by hand. `internal/unicodecase` is
//! generated from this dump, and its test replays the dump in full.
//!
//! Usage:  mj-casegen   # JSON document on stdout
//!
//! The Unicode version is the one the Rust toolchain that runs this carries,
//! which is also the one the harness's engine build uses. It is recorded in
//! the output so a drift shows up as a diff rather than as a mystery.

use std::fmt::Write as _;

const SCHEMA_VERSION: u32 = 1;

/// Inclusive scalar ranges over which a boolean property holds.
fn ranges(pred: impl Fn(char) -> bool) -> Vec<[u32; 2]> {
    let mut rv: Vec<[u32; 2]> = Vec::new();
    let mut start: Option<u32> = None;
    for cp in 0..=0x10FFFFu32 {
        let held = match char::from_u32(cp) {
            Some(c) => pred(c),
            None => false,
        };
        match (held, start) {
            (true, None) => start = Some(cp),
            (false, Some(s)) => {
                rv.push([s, cp - 1]);
                start = None;
            }
            _ => {}
        }
    }
    if let Some(s) = start {
        rv.push([s, 0x10FFFF]);
    }
    rv
}

/// Every scalar whose mapping is not the identity, as (scalar, mapping).
fn mappings(map: impl Fn(char) -> String) -> Vec<(u32, String)> {
    let mut rv = Vec::new();
    for cp in 0..=0x10FFFFu32 {
        if let Some(c) = char::from_u32(cp) {
            let mapped = map(c);
            if mapped.chars().count() != 1 || mapped.chars().next() != Some(c) {
                rv.push((cp, mapped));
            }
        }
    }
    rv
}

fn json_string(s: &str) -> String {
    let mut rv = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => rv.push_str("\\\""),
            '\\' => rv.push_str("\\\\"),
            c if (c as u32) < 0x20 => write!(rv, "\\u{:04x}", c as u32).unwrap(),
            c => rv.push(c),
        }
    }
    rv.push('"');
    rv
}

fn emit_ranges(out: &mut String, name: &str, rs: &[[u32; 2]], last: bool) {
    write!(out, "  \"{name}\": [").unwrap();
    for (i, r) in rs.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(out, "[{},{}]", r[0], r[1]).unwrap();
    }
    out.push(']');
    if !last {
        out.push(',');
    }
    out.push('\n');
}

fn emit_mappings(out: &mut String, name: &str, ms: &[(u32, String)]) {
    write!(out, "  \"{name}\": [").unwrap();
    for (i, (cp, s)) in ms.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(out, "[{},{}]", cp, json_string(s)).unwrap();
    }
    out.push_str("],\n");
}

/// Strings whose case mapping or trimming is context sensitive, or where the
/// Go and Rust standard libraries are known to part ways.
const SAMPLES: &[&str] = &[
    "",
    "hello",
    "HELLO",
    "\u{df}",                 // sharp s: uppercases to two characters
    "gru\u{df}e",             // ... in the middle of a word
    "\u{130}",                // dotted capital I: lowercases to two scalars
    "\u{131}",                // dotless i
    "\u{3a3}",                // lone capital sigma
    "\u{3a3}\u{3a3}",         // two sigmas: the second one is word final
    "\u{391}\u{3a3}",         // alpha sigma: word final
    "\u{391}\u{3a3}\u{391}",  // ... not word final
    "\u{391}\u{3a3}'",        // apostrophe is case ignorable, so still final
    "\u{391}\u{3a3}\u{301}",  // combining acute is case ignorable
    "\u{391}\u{3a3}\u{301}A", // ... and a cased char follows it
    "\u{fb03}",               // ffi ligature
    "\u{1f80}",               // Greek with ypogegrammeni
    "\u{149}",                // n preceded by apostrophe
    "\u{587}",                // Armenian ech-yiwn ligature
    "\u{a0}x\u{a0}",          // no-break space is Unicode whitespace
    "\u{2028}x\u{2029}",      // line and paragraph separators
    "\u{85}x\u{85}",          // next line
    "\u{feff}x",              // zero width no-break space is NOT whitespace
    " \t\r\n\u{b}\u{c}x ",
    "\u{1e9e}",               // capital sharp s
    "\u{fb00}\u{fb01}",       // ff fi
];

/// The scalars `Debug for str` special-cases into a short escape rather than
/// into `\u{...}`. They are handled in code on the Go side, so the generated
/// table only has to carry the `\u{...}` decision.
///
/// `impl Debug for str` calls `escape_debug_ext` with
/// `EscapeDebugExtArgs { escape_grapheme_extended: true, escape_single_quote:
/// false, escape_double_quote: true }` for EVERY character, which is why a
/// single scalar decides the whole thing and no context is needed.
const DEBUG_SHORT_ESCAPES: &[(char, &str)] = &[
    ('\0', "\\0"),
    ('\t', "\\t"),
    ('\r', "\\r"),
    ('\n', "\\n"),
    ('\\', "\\\\"),
    ('"', "\\\""),
];

/// The `Debug for str` form of one scalar, without the surrounding quotes.
fn debug_escaped(c: char) -> String {
    let quoted = format!("{:?}", String::from(c));
    quoted[1..quoted.len() - 1].to_string()
}

/// Inclusive ranges of the scalars `Debug for str` renders as `\u{...}`.
///
/// That is `is_grapheme_extended(c) || !is_printable(c)` minus the short
/// escapes above, but it is derived from the reference's own output rather than
/// from those predicates, neither of which is a stable public API.
fn debug_unicode_escape_ranges() -> Vec<[u32; 2]> {
    ranges(|c| debug_escaped(c).starts_with("\\u{"))
}

/// Fails the generator if any scalar escapes into something that is neither a
/// listed short escape nor `\u{...}`, so a Rust release that grew a new escape
/// form cannot be absorbed silently by a table that only models two shapes.
fn assert_debug_escape_shapes() {
    for cp in 0..=0x10FFFFu32 {
        let Some(c) = char::from_u32(cp) else { continue };
        let escaped = debug_escaped(c);
        if escaped == String::from(c) || escaped.starts_with("\\u{") {
            continue;
        }
        if DEBUG_SHORT_ESCAPES.iter().any(|(k, v)| *k == c && *v == escaped) {
            continue;
        }
        panic!("U+{cp:04X} debug-escapes as {escaped:?}, which is neither a known short escape nor \\u{{...}}");
    }
}

/// Whole strings, so the Go port is checked against `Debug for str` itself and
/// not only against the per-scalar decision it is built from.
const DEBUG_SAMPLES: &[&str] = &[
    "",
    "hello",
    "a\"b",
    "a\\b",
    "a\0b",
    "a\tb\rc\nd",
    "it's",
    "\u{e9}",           // precomposed e-acute: printable, left alone
    "e\u{301}",         // ...and its decomposition, whose mark IS escaped
    "i\u{307}",
    "\u{130}",
    "a\u{a0}b",         // no-break space
    "a\u{200d}b",       // zero width joiner
    "a\u{ad}b",         // soft hyphen
    "\u{1f600}",        // emoji: printable
    "\u{1f1fa}\u{1f1f8}",
    "a\u{903}b",        // Mc, not Grapheme_Extend: NOT escaped
    "a\u{9be}b",        // Mc but Other_Grapheme_Extend: escaped
    "a\u{20dd}b",       // Me
    "a\u{e0100}b",      // supplementary variation selector
    "a\u{378}b",        // unassigned
    "a\u{e000}b",       // private use
    "a\u{2028}b\u{2029}c",
    "\u{7f}\u{80}\u{9f}",
    "\u{feff}",
    "\u{3000}",
    "caf\u{e9} \u{4e2d}\u{6587}",
];

/// Strings whose UniCase ordering is what `sort` and `groupby` actually ask
/// for. Every ordered pair of these is dumped, so the Go port is checked
/// against real comparisons and not only against the per-scalar fold table.
const CASE_CMP_SAMPLES: &[&str] = &[
    "",
    "a",
    "A",
    "b",
    "B",
    "ab",
    "AB",
    "aB",
    "abc",
    "\u{df}",          // sharp s folds to "ss"
    "ss",
    "SS",
    "Ma\u{df}e",
    "MASSE",
    "masse",
    "i",
    "I",
    "\u{130}",         // dotted capital I folds to "i" + combining dot
    "i\u{307}",        // i + combining dot above
    "\u{131}",         // dotless i
    "\u{3c3}",         // sigma
    "\u{3c2}",         // final sigma
    "\u{3a3}",         // capital sigma
    "\u{3c3}\u{3c4}\u{3b9}\u{3b3}\u{3bc}\u{3b1}\u{3c2}",
    "\u{3a3}\u{3a4}\u{399}\u{393}\u{39c}\u{391}\u{3a3}",
    "\u{fb01}",        // fi ligature
    "fi",
    "\u{fb03}",        // ffi ligature: Fold::Three, whose iteration order is
    "ffi",             // NOT the folded order -- see unicodecase.FoldCompare
    "\u{212a}",        // Kelvin sign folds to "k"
    "k",
    "K",
    "\u{1fb2}",
    "\u{1f70}\u{3b9}",
    "\u{e9}",          // e-acute
    "\u{c9}",
    "e",
    "E",
];

fn main() {
    let mut out = String::from("{\n");
    write!(out, "  \"schema_version\": {SCHEMA_VERSION},\n").unwrap();
    write!(
        out,
        "  \"rustc\": {},\n",
        json_string(option_env!("MJ_RUSTC_VERSION").unwrap_or("unknown"))
    )
    .unwrap();

    // The tables are of the STRING-level operation applied to one scalar, not
    // of char::to_uppercase, because `|upper` is `str::to_uppercase` and the
    // two are not the same function: the string form has one context-sensitive
    // rule (final sigma) and takes a shortcut for characters that are already
    // lowercase. Tabulating the string form means the Go port reproduces it by
    // construction, with only the sigma rule left to implement.
    emit_mappings(
        &mut out,
        "to_uppercase",
        &mappings(|c| String::from(c).to_uppercase()),
    );
    emit_mappings(
        &mut out,
        "to_lowercase",
        &mappings(|c| String::from(c).to_lowercase()),
    );

    emit_ranges(&mut out, "lowercase", &ranges(char::is_lowercase), false);
    emit_ranges(&mut out, "uppercase", &ranges(char::is_uppercase), false);
    emit_ranges(&mut out, "alphabetic", &ranges(char::is_alphabetic), false);
    emit_ranges(&mut out, "numeric", &ranges(char::is_numeric), false);
    emit_ranges(&mut out, "whitespace", &ranges(char::is_whitespace), false);

    // `str::to_lowercase` decides between sigma and final sigma with the
    // Unicode `Cased` and `Case_Ignorable` properties, and `char` does not
    // expose either. Both are recovered from the reference implementation's
    // own behaviour instead of being reconstructed from the spec:
    //
    //   "ΑΣx"  lowercases the sigma to σ exactly when x is Cased
    //   "ΑΣxA" lowercases the sigma to σ exactly when x is Cased or
    //          Case_Ignorable
    //
    // because the rule scans past case-ignorables and then asks whether what
    // follows is cased. The sigma needs a cased character before it, or it is
    // never word final and the probe cannot distinguish anything.
    let sigma_lowered_to_plain = |probe: String| {
        probe.to_lowercase().chars().nth(1) == Some('\u{3c3}')
    };
    let is_cased = |c: char| sigma_lowered_to_plain(format!("\u{391}\u{3a3}{c}"));
    let is_ignorable =
        |c: char| !is_cased(c) && sigma_lowered_to_plain(format!("\u{391}\u{3a3}{c}A"));
    emit_ranges(&mut out, "cased", &ranges(is_cased), false);
    emit_ranges(&mut out, "case_ignorable", &ranges(is_ignorable), false);

    // The engine's case-insensitive comparator is `UniCase`, not "lowercase
    // both sides": `cmp_helper` compares `UniCase::new(a)` to `UniCase::new(b)`
    // under the `unicode` feature (filters.rs:284-300), and UniCase orders by
    // the Unicode case-FOLDED character sequence. Folding is not lowercasing —
    // "ß" folds to "ss" and "İ" folds to two scalars — so `sort` and `groupby`
    // cannot be reproduced with a lowercase-and-compare.
    //
    // `to_folded_case` on the Unicode encoding is `chars().flat_map(lookup)`,
    // which is character for character the sequence `Ord` compares, so this
    // tabulates the comparator's own input rather than a reconstruction of it.
    emit_mappings(
        &mut out,
        "unicase_fold",
        &mappings(|c| unicase::UniCase::unicode(String::from(c)).to_folded_case()),
    );

    // `Debug for str` is what the engine renders a string INSIDE a container
    // with — a list, a map, `pprint`, `debug()` — and it is not Go's `%q`. Two
    // things differ: the escape syntax is `\u{a0}` where Go writes ` `,
    // `\x7f` or `\U000e0020`; and Rust escapes every GRAPHEME-EXTENDED scalar,
    // so "e" + U+0301 debug-prints as `e\u{301}` while Go prints `é`. Both are
    // prompt bytes wherever a container holds a non-ASCII string.
    //
    // Only the `\u{...}` DECISION is tabulated. It covers every unassigned and
    // private-use scalar, so a per-scalar map would be enormous, while as
    // inclusive ranges it is the same shape as the property tables above. The
    // six short escapes are a closed set, asserted here and written in code on
    // the Go side.
    assert_debug_escape_shapes();
    emit_ranges(
        &mut out,
        "debug_unicode_escape",
        &debug_unicode_escape_ranges(),
        false,
    );

    out.push_str("  \"debug_samples\": [");
    for (i, sample) in DEBUG_SAMPLES.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(
            out,
            "[{},{}]",
            json_string(sample),
            json_string(&format!("{sample:?}"))
        )
        .unwrap();
    }
    out.push_str("],\n");

    // Real orderings, over the strings a case-insensitive sort actually has to
    // separate. Ordered pairs, so asymmetry would show.
    out.push_str("  \"unicase_cmp\": [");
    let mut first = true;
    for a in CASE_CMP_SAMPLES {
        for b in CASE_CMP_SAMPLES {
            if !first {
                out.push(',');
            }
            first = false;
            let ordering = match unicase::UniCase::new(*a).cmp(&unicase::UniCase::new(*b)) {
                std::cmp::Ordering::Less => -1,
                std::cmp::Ordering::Equal => 0,
                std::cmp::Ordering::Greater => 1,
            };
            write!(
                out,
                "[{},{},{}]",
                json_string(a),
                json_string(b),
                ordering
            )
            .unwrap();
        }
    }
    out.push_str("],\n");

    // Multi-character cases, so the Go port is checked against the string-level
    // operation and not only against the per-scalar tables it is built from.
    out.push_str("  \"samples\": [");
    for (i, sample) in SAMPLES.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        write!(
            out,
            "[{},{},{},{},{},{}]",
            json_string(sample),
            json_string(&sample.to_uppercase()),
            json_string(&sample.to_lowercase()),
            json_string(sample.trim()),
            json_string(sample.trim_start()),
            json_string(sample.trim_end()),
        )
        .unwrap();
    }
    out.push_str("]\n");

    out.push_str("}\n");
    print!("{out}");
}
