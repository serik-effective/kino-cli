package kinozal

import "testing"

func TestParseTitleMovie(t *testing.T) {
	it := Item{RawTitle: "Мятеж / Mutiny / 2026 / ПМ, СТ / 4K, SDR / WEB-DLRip (2160p)"}
	if !parseTitle(&it) {
		t.Fatal("movie rejected")
	}
	if it.TitleRU != "Мятеж" || it.TitleOrig != "Mutiny" || it.Year != 2026 {
		t.Errorf("got %q / %q / %d", it.TitleRU, it.TitleOrig, it.Year)
	}
	if it.Quality != "WEB-DLRip (2160p)" || it.Tags != "ПМ, СТ, 4K, SDR" {
		t.Errorf("quality=%q tags=%q", it.Quality, it.Tags)
	}
}

func TestParseTitleRussianOnly(t *testing.T) {
	it := Item{RawTitle: "Новенькая / 1968 / РУ / DVDRip"}
	if !parseTitle(&it) {
		t.Fatal("movie rejected")
	}
	if it.TitleRU != "Новенькая" || it.TitleOrig != "" || it.Year != 1968 {
		t.Errorf("got %q / %q / %d", it.TitleRU, it.TitleOrig, it.Year)
	}
}

func TestParseTitleRejectsNonMovies(t *testing.T) {
	bad := []string{
		"Число зверя (1 сезон: 1-12 серии из 12) / 2024 / РУ, СТ / WEB-DLRip",
		"Сны (За закрытыми глазами) (1-8 серии из 8) / 2022 / РУ / WEB-DLRip",
		"Лучшие мировые хиты: Instrumental Gold Collection Том 1-7 (14CD) / Instrumental / 1995-2001 / MP3",
		"Юрий Винокуров - Первый среди равных (16 книг из 16) / Фэнтези / 2024-2026 / MP3",
		"Джеймс Бонд 007 (Бондиана) (Коллекция) / James Bond 007: Collection / 1962-2021 / BDRip",
		"S.T.A.L.K.E.R. 2: Heart of Chornobyl (Ultimate Edition) / x64 / RU / Action / 2024 / Portable / PC (Windows)",
	}
	for _, raw := range bad {
		it := Item{RawTitle: raw}
		if parseTitle(&it) {
			t.Errorf("accepted non-movie: %s", raw)
		}
	}
}

func TestParseDetailsCounters(t *testing.T) {
	body := `<div>Раздают 246 <span>Скачивают 1</span> Скачали 13 444 Комментариев 184</div>`
	st := ParseDetails(body)
	if st.Seeds != 246 || st.Leechers != 1 || st.Downloads != 13444 || st.Comments != 184 {
		t.Errorf("got %+v", st)
	}
}
