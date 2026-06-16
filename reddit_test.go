package main

import "testing"

func TestParseThreadsJSON(t *testing.T) {
	body := []byte(`{"data":{"children":[
		{"data":{"id":"a1","title":"  Hot one  ","permalink":"/r/x/comments/a1/","author":"u1","num_comments":5,"stickied":false}},
		{"data":{"id":"p1","title":"Pinned","permalink":"/r/x/comments/p1/","author":"mod","num_comments":2,"stickied":true}}
	]}}`)
	threads, err := parseThreadsJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("want 2 threads, got %d", len(threads))
	}
	// Stickied first.
	if !threads[0].stickied || threads[0].id != "p1" {
		t.Errorf("stickied thread should sort first, got %q", threads[0].id)
	}
	// Title trimmed, permalink absolutized, counts carried.
	if threads[1].title != "Hot one" {
		t.Errorf("title not trimmed: %q", threads[1].title)
	}
	if threads[1].permalink != "https://www.reddit.com/r/x/comments/a1/" {
		t.Errorf("permalink not absolutized: %q", threads[1].permalink)
	}
	if threads[1].numComments != 5 || threads[1].author != "u1" {
		t.Errorf("thread fields wrong: %+v", threads[1])
	}
}

func TestParseThreadsJSONEmpty(t *testing.T) {
	if _, err := parseThreadsJSON([]byte(`{"data":{"children":[]}}`)); err == nil {
		t.Error("empty listing should error")
	}
}

func TestParseThreadJSON(t *testing.T) {
	body := []byte(`[
		{"data":{"children":[{"data":{
			"title":"OP title","author":"op","selftext":"hello **world**","score":42,"created_utc":1700000000,"is_self":true
		}}]}},
		{"data":{"children":[
			{"kind":"t1","data":{"id":"c1","author":"a","body":"top comment","score":10,"created_utc":1700000100,"parent_id":"t3_x","replies":{"data":{"children":[
				{"kind":"t1","data":{"id":"c2","author":"b","body":"a reply","score":3,"created_utc":1700000050,"parent_id":"t1_c1","replies":""}}
			]}}}},
			{"kind":"more","data":{"id":"more1"}},
			{"kind":"t1","data":{"id":"c3","author":"c","body":"[deleted]","score":0,"created_utc":1700000200,"parent_id":"t3_x"}}
		]}}
	]`)

	op, comments, err := parseThreadJSON(body)
	if err != nil {
		t.Fatal(err)
	}

	// OP from listings[0].
	if op == nil {
		t.Fatal("expected an OP")
	}
	if op.title != "OP title" || op.author != "op" || op.score != 42 || !op.hasScore {
		t.Errorf("OP fields wrong: %+v", op)
	}

	// "more" stub and [deleted] comment are dropped; nested reply is kept.
	if len(comments) != 2 {
		t.Fatalf("want 2 comments (c1 + nested c2), got %d", len(comments))
	}
	// Oldest-first: c2 (created 1700000050) before c1 (1700000100).
	if comments[0].id != "t1_c2" || comments[1].id != "t1_c1" {
		t.Errorf("comments not sorted oldest-first: %q, %q", comments[0].id, comments[1].id)
	}
	// Reply threading is preserved.
	if comments[0].parentID != "t1_c1" {
		t.Errorf("nested reply parentID wrong: %q", comments[0].parentID)
	}
	for _, c := range comments {
		if c.body == "[deleted]" {
			t.Error("[deleted] comment should be dropped")
		}
	}
}
