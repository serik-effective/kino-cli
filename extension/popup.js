const STORAGE_KEY = "kino_films";

const $ = (id) => document.getElementById(id);

// Filters are applied when exporting, not when capturing: everything seen is
// kept, so changing your mind about the threshold does not mean browsing again.
//
// Filtering by rating is off by default and deliberately so. A card exposes its
// rating only rarely — 64 of 788 in the reference harvest — so a rating gate
// throws away most of what was collected, and the ids are the valuable part.
// Quality is better judged in kino itself, once "refresh ratings" has filled in
// real numbers with real vote counts behind them.
function filtered(all, minRating, years) {
  const floor = new Date().getFullYear() - years;
  return Object.values(all).filter((f) => {
    if (f.year && Number(f.year) < floor) return false;
    if (minRating > 0) {
      // Treating "unknown" as "good enough" would poison the set, so a film
      // with no visible rating fails an explicit rating gate.
      if (!f.rating) return false;
      if (Number(f.rating) < minRating) return false;
    }
    return true;
  });
}

// ratingGate is the threshold actually in force: zero unless asked for.
function ratingGate() {
  return $("useRating").checked ? Number($("minRating").value) : 0;
}

// toDiscovery emits exactly the shape "kino import kp" already understands, so
// the harvest needs no new code on the Go side.
function toDiscovery(films) {
  const out = {};
  films.forEach((f, i) => {
    out[`${f.id},, 0, ext-${i}`] = {
      film: {
        id: f.id,
        title: f.title,
        originalTitle: f.orig || null,
        year: f.year || "",
        type: f.type || "MOVIE",
        country: { name: "" },
        genres: [],
        // count stays 0: a card shows the rating but not how many people gave
        // it. The importer treats 0 as "no value" and leaves what it has, and
        // "kino refresh ratings" fills the real numbers for free afterwards.
        kinopoiskRating: { value: f.rating || "", count: 0 },
        // onlineViewOption is deliberately absent, not empty: this helper has
        // no idea whether a film is streaming, and an empty object would read
        // as "not available" and wipe what the player payload knows.
      },
      views: 0,
    };
  });
  return out;
}

function download(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/json" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

function stamp() {
  return new Date().toISOString().slice(0, 10);
}

function render(all) {
  const films = Object.values(all);
  $("total").textContent = films.length;

  const withRating = films.filter((f) => f.rating).length;
  const withYear = films.filter((f) => f.year).length;
  const shows = films.filter((f) => f.type === "SHOW").length;
  const passing = filtered(all, ratingGate(), Number($("years").value)).length;

  $("breakdown").innerHTML = `
    <tr><td>с рейтингом</td><td class="n">${withRating}</td></tr>
    <tr><td>с годом</td><td class="n">${withYear}</td></tr>
    <tr><td>сериалов (импорт их пропустит)</td><td class="n">${shows}</td></tr>
    <tr><td><b>пройдёт фильтр</b></td><td class="n"><b>${passing}</b></td></tr>`;
}

function withAll(fn) {
  chrome.storage.local.get(STORAGE_KEY, (data) => fn(data[STORAGE_KEY] || {}));
}

for (const id of ["minRating", "years"]) {
  $(id).addEventListener("input", () => withAll(render));
}
$("useRating").addEventListener("change", () => {
  $("minRating").disabled = !$("useRating").checked;
  withAll(render);
});

$("export").onclick = () =>
  withAll((all) => {
    const films = filtered(all, ratingGate(), Number($("years").value));
    if (!films.length) return;
    download(`kp-${stamp()}.json`, JSON.stringify(toDiscovery(films), null, 1));
  });

$("exportAll").onclick = () =>
  withAll((all) => {
    const films = Object.values(all);
    if (!films.length) return;
    download(`kp-all-${stamp()}.json`, JSON.stringify(toDiscovery(films), null, 1));
  });

$("clear").onclick = () => {
  if (!confirm("Забыть все собранные фильмы?")) return;
  chrome.storage.local.set({ [STORAGE_KEY]: {} }, () => withAll(render));
};

withAll(render);
