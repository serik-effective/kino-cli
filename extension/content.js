// Reads the Kinopoisk page the user has already opened and remembers the films
// visible on it.
//
// This is deliberately not a crawler. It issues no network requests of its own,
// follows no links, turns no pages and runs on no schedule: it only looks at
// what the browser has already rendered because a person navigated there. That
// is why the manifest asks for no network permission at all — the extension
// could not fetch anything even if the code tried.
//
// What it is really after is the pairing of a Kinopoisk id with a title. That
// pairing is the expensive part: the paid lookup allows 200 a day, while
// ratings and vote counts can be filled in afterwards for free.

const STORAGE_KEY = "kino_films";

// Kinopoisk ships obfuscated, frequently changing class names, so nothing here
// depends on them. A film link is the one structural fact the markup cannot
// avoid: every card is an anchor to /film/<id>/ or /series/<id>/.
const FILM_HREF = /\/(film|series)\/(\d+)\//;

// The link text is where the useful metadata actually lives, and it comes in a
// handful of shapes. These were derived from a real harvest of 788 cards, not
// guessed:
//
//   Побег из Шоушенка (The Shawshank Redemption1994)   list card
//   Друзья (сериал) (Friends1994 – 2004)               list card, series
//   Ветка сирени (2007)                                no original title
//   Бригада. 2002, драма                               search result
//   Затмение                                           sidebar teaser, no year
//
// Note the year is glued to the original title with no separator, which is why
// it is captured as its own group rather than split on whitespace.
const JUNK = /^\d+\.?$|топ\s*250|Рейтинг Кинопоиска/i;
const KIND = /\s*\((сериал|мини-сериал|видео|ТВ)\)\s*/i;
const PARENS = /^(.*?)\s*\(([^()]*?)(\d{4})(?:\s*[–—-]\s*\d{0,4})?\)\s*$/;
const DOTTED = /^(.*?)\.\s*(\d{4})(?:\s*[–—-]\s*\d{0,4})?\s*,\s*(.+)$/;

// A rating is a number like 7.3 standing on its own. Years have no decimal
// point, so they cannot collide. This is best-effort and usually fails: only 64
// of 788 cards in the reference harvest exposed one. That is fine — the rating
// is not what this tool is for, and "kino refresh ratings" fills it properly
// and for free once the id is known.
const RATING_RE = /(?:^|[^\d.,])((?:10|[1-9])[.,]\d)(?![\d.,])/;

// howFarUp is how many ancestors to inspect looking for a rating near the link.
const HOW_FAR_UP = 4;

// parseTitle splits a card's link text into the parts we store. It returns null
// for navigation chrome and bare list numbers, which share the markup of a real
// card but are not films.
function parseTitle(raw) {
  const s0 = (raw || "").trim().replace(/\s+/g, " ");
  if (!s0 || JUNK.test(s0)) return null;

  const kind = s0.match(KIND);
  // "(видео)" marks a concert or a music release, not a series.
  const isShow = !!kind && kind[1].toLowerCase() !== "видео";
  const s = s0.replace(KIND, " ").trim();

  let m = s.match(PARENS);
  if (m) return { title: m[1].trim(), orig: m[2].trim(), year: m[3], isShow };

  m = s.match(DOTTED);
  if (m) return { title: m[1].trim(), orig: "", year: m[2], isShow };

  return s ? { title: s, orig: "", year: "", isShow } : null;
}

function parseFilms() {
  const found = new Map();

  for (const a of document.querySelectorAll('a[href*="/film/"], a[href*="/series/"]')) {
    const m = a.getAttribute("href").match(FILM_HREF);
    if (!m) continue;

    const id = Number(m[2]);
    if (!id || found.has(id)) continue;

    const parsed = parseTitle(a.innerText) || parseTitle(a.querySelector("img")?.alt);
    if (!parsed) continue;

    const card = climbToCard(a);
    found.set(id, {
      id,
      title: parsed.title,
      orig: parsed.orig,
      year: parsed.year,
      type: parsed.isShow || m[1] === "series" ? "SHOW" : "MOVIE",
      rating: firstMatch(card ? card.innerText || "" : "", RATING_RE),
      seenAt: new Date().toISOString().slice(0, 10),
      from: location.pathname,
    });
  }
  return [...found.values()];
}

// climbToCard walks up from the link looking for an ancestor that also carries a
// rating. It stops early so it cannot swallow a neighbouring card.
function climbToCard(a) {
  let node = a;
  for (let i = 0; i < HOW_FAR_UP && node.parentElement; i++) {
    node = node.parentElement;
    if (RATING_RE.test(node.innerText || "")) return node;
  }
  return node;
}

function firstMatch(text, re) {
  const m = text.match(re);
  return m ? m[1].replace(",", ".") : "";
}

// merge keeps the richer record: a later page may show a rating the first one
// lacked, and losing that would mean asking the user to browse again.
function merge(stored, fresh) {
  const out = { ...stored };
  for (const f of fresh) {
    const old = out[f.id];
    if (!old) {
      out[f.id] = f;
      continue;
    }
    out[f.id] = {
      ...old,
      title: old.title || f.title,
      orig: old.orig || f.orig,
      rating: old.rating || f.rating,
      year: old.year || f.year,
      type: f.type === "SHOW" ? "SHOW" : old.type,
    };
  }
  return out;
}

function harvest() {
  const fresh = parseFilms();
  if (!fresh.length) return;
  chrome.storage.local.get(STORAGE_KEY, (data) => {
    const merged = merge(data[STORAGE_KEY] || {}, fresh);
    chrome.storage.local.set({ [STORAGE_KEY]: merged });
  });
}

harvest();

// Kinopoisk renders results progressively, so the same page keeps growing as
// the user scrolls. Re-reading on mutation is what makes scrolling enough.
let pending = null;
new MutationObserver(() => {
  clearTimeout(pending);
  pending = setTimeout(harvest, 800);
}).observe(document.body, { childList: true, subtree: true });
