package kp

import "testing"

const sample = `{
  "8290235,, 0, rnd-0.86": {
    "streamUrl": "https://strm.yandex.ru/vod/secret/master.m3u8?ottsessionid=deadbeef",
    "filmId": 8290235,
    "film": {
      "id": 8290235, "title": "Твоё сердце будет разбито", "originalTitle": null,
      "genres": ["мелодрама"], "year": "2026", "type": "MOVIE",
      "country": {"name": "Россия"},
      "kinopoiskRating": {"value": "7.5", "count": 587194},
      "onlineViewOption": {"text": "С мультиподпиской Яндекс Плюс", "isAvailableOnline": true}
    },
    "views": 226931
  },
  "8672609,, 0, rnd-0.74": {
    "filmId": 8672609,
    "film": {
      "id": 8672609, "title": "Гудовы", "year": "с 2026", "type": "SHOW",
      "country": {"name": "Россия"},
      "kinopoiskRating": {"value": "7.7", "count": 13443},
      "onlineViewOption": {"isAvailableOnline": true, "text": "С мультиподпиской Яндекс Плюс"}
    },
    "views": 2213
  }
}
{
  "5453090,, 0, rnd-0.28": {
    "filmId": 5453090,
    "film": {
      "id": 5453090, "title": "Тамерлан", "originalTitle": "Rise of the Conqueror",
      "year": "2026", "type": "MOVIE", "country": {"name": "Узбекистан"},
      "kinopoiskRating": {"value": "7.3", "count": 66671},
      "onlineViewOption": {}
    },
    "views": 14720
  }
}`

func TestParseDiscovery(t *testing.T) {
	items, err := ParseDiscovery([]byte(sample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 across both objects", len(items))
	}

	byID := map[int]DiscoveryItem{}
	for _, it := range items {
		byID[it.KPID] = it
	}

	film := byID[8290235]
	if film.Title != "Твоё сердце будет разбито" || film.Year != 2026 || !film.IsMovie() {
		t.Errorf("movie parsed as %+v", film)
	}
	if film.Rating != 7.5 || film.Votes != 587194 || film.Views != 226931 {
		t.Errorf("counters = %v/%d/%d", film.Rating, film.Votes, film.Views)
	}
	if !film.Online || film.Offer != "С мультиподпиской Яндекс Плюс" {
		t.Errorf("availability = %v %q", film.Online, film.Offer)
	}

	show := byID[8672609]
	if show.IsMovie() {
		t.Error("SHOW must not be reported as a movie")
	}
	if show.Year != 2026 {
		t.Errorf(`year "с 2026" parsed as %d`, show.Year)
	}

	uz := byID[5453090]
	if uz.OrigName != "Rise of the Conqueror" || uz.Country != "Узбекистан" {
		t.Errorf("original title / country = %q / %q", uz.OrigName, uz.Country)
	}
	if uz.Online || uz.Offer != "" {
		t.Errorf("empty onlineViewOption must mean not available, got %v %q", uz.Online, uz.Offer)
	}
}

// A harvest that says nothing about availability must be distinguishable from
// one that says "not available": the first must never clear what the player
// payload established.
func TestOnlineKnownSeparatesSilenceFromAbsence(t *testing.T) {
	body := []byte(`{
"1,, 0, a": {"film": {"id": 1, "title": "Из плеера", "type": "MOVIE",
  "onlineViewOption": {"isAvailableOnline": false}}, "views": 0},
"2,, 0, b": {"film": {"id": 2, "title": "Из расширения", "type": "MOVIE"}, "views": 0}
}`)
	items, err := ParseDiscovery(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	byID := map[int]DiscoveryItem{}
	for _, it := range items {
		byID[it.KPID] = it
	}
	if !byID[1].OnlineKnown {
		t.Error("a payload carrying onlineViewOption must count as knowing")
	}
	if byID[1].Online {
		t.Error("isAvailableOnline:false must mean not available")
	}
	if byID[2].OnlineKnown {
		t.Error("a record without onlineViewOption must not claim to know")
	}
}
