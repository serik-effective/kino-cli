package kp

import "testing"

func TestParseRatingXMLBothRatings(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="WINDOWS-1251" standalone="yes"?>` +
		`<rating><kp_rating num_vote="2588665">8.686</kp_rating><imdb_rating num_vote="454000">7.8</imdb_rating></rating>`)
	r, err := parseRatingXML(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.KP == nil || *r.KP != 8.686 || r.KPVotes == nil || *r.KPVotes != 2588665 {
		t.Errorf("kp = %v/%v", deref(r.KP), derefI(r.KPVotes))
	}
	if r.IMDb == nil || *r.IMDb != 7.8 || r.IMDbVotes == nil || *r.IMDbVotes != 454000 {
		t.Errorf("imdb = %v/%v", deref(r.IMDb), derefI(r.IMDbVotes))
	}
}

func TestParseRatingXMLKPOnly(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="WINDOWS-1251" standalone="yes"?>` +
		`<rating><kp_rating num_vote="587000">7.457</kp_rating></rating>`)
	r, err := parseRatingXML(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r.KP == nil || *r.KP != 7.457 {
		t.Errorf("kp = %v", deref(r.KP))
	}
	if r.IMDb != nil || r.IMDbVotes != nil {
		t.Errorf("imdb should be absent, got %v", deref(r.IMDb))
	}
}

func TestParseRatingXMLPlaceholders(t *testing.T) {
	body := []byte(`<rating><kp_rating num_vote="0">null</kp_rating><imdb_rating num_vote="0">-1</imdb_rating></rating>`)
	if _, err := parseRatingXML(body); err == nil {
		t.Fatal("placeholder ratings accepted; want error")
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func derefI(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
