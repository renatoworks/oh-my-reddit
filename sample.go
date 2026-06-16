package main

import (
	"fmt"
	"math/rand"
	"time"
)

// sampleThreadTitle / sampleSubreddit label the demo feed so the UI looks
// real before any network wiring.
const (
	sampleThreadTitle = "Match Thread: it's all kicking off"
	sampleSubreddit   = "r/soccer"
)

// sampleAuthors fabricate plausible handles for the demo thread.
var sampleAuthors = []string{
	"xG_enjoyer", "offside_trap", "park_the_bus", "tiki_taka_andy",
	"VAR_was_right", "gegenpress", "false_nine", "stoppage_time",
	"clean_sheet", "wonderkid_watch", "the_gaffer", "set_piece_merchant",
}

// demoPool is a themed set of comment bodies drawn from a shuffle bag: each pass
// hands out every line once in a random order before any repeats, and reshuffles
// (avoiding an immediate repeat across the boundary) when exhausted. So a burst
// never shows the same line twice, and the order differs every time around.
type demoPool struct {
	lines []string
	bag   []int
	last  int
}

func newPool(lines ...string) *demoPool {
	return &demoPool{lines: lines, last: -1}
}

func (p *demoPool) next() string {
	if len(p.bag) == 0 {
		p.bag = rand.Perm(len(p.lines))
		if len(p.bag) > 1 && p.bag[0] == p.last {
			p.bag[0], p.bag[1] = p.bag[1], p.bag[0] // don't repeat across the reshuffle
		}
	}
	i := p.bag[0]
	p.bag, p.last = p.bag[1:], i
	return p.lines[i]
}

// Themed comment pools. The demo plays these out as a match "arc" (see demoArc)
// so the chat clusters around what's happening — a goal sets off a rush of goal
// reactions, a bad call sets off a refereeing pile-on — instead of unrelated
// one-liners arriving in a flat rotation.
var (
	demoChatter = newPool(
		"anyone got a stream that isn't 47 ads and a virus",
		"lineup's out and i already hate it",
		"co-commentator sounds like he's reading a hostage note",
		"decent crowd, shame about the football",
		"0-0 and i've already aged five years",
		"tuned in late, what did i miss (nothing, apparently)",
		"my fantasy team is depending on this man please",
		"midfield battle this, proper chess if chess was boring",
		"meh",
		"tuned in for the highlights and stayed for the existential dread, classic me",
	)
	demoBuildUp = newPool(
		"oooh that's a lovely ball in",
		"corner. statistically nothing happens but i believe",
		"counter!! everyone GO i can't watch this",
		"he's through!! HE'S THROUGH",
		"how is that not a goal i'm actually sweating",
		"cross it man my nan could finish that",
		"ooh",
		"that is a glorious ball in and if he doesn't bury it i am uninstalling the stream",
	)
	demoGoal = newPool(
		// Lengths deliberately all over the place — one-word screams up to rambling
		// paragraphs — and 30+ lines so a full eruption (23 comments) never repeats.
		"GOAL",
		"send that man straight to the louvre, frame him next to the mona lisa",
		"GOOOAAALLL",
		"OH MY DAYS",
		"i was quietly making a sandwich and i am now somehow on the kitchen floor",
		"GET INNNN",
		"top bins, keeper was a tourist",
		"WE'RE SO BACK",
		"explaining to my wife, for the third time tonight, why a man kicking a ball has me sobbing",
		"YESSS",
		"UNBELIEVABLE SCENES HERE",
		"i've completely lost my voice and it's not even half time yet",
		"the away end has physically detached from the stadium and is now in orbit",
		"absolute screamer, didn't even see it move",
		"WHAT",
		"i'm forgiving him for the last 89 minutes of doing absolutely nothing",
		"SCENES. ACTUAL SCENES",
		"poetry. that was actual poetry",
		"THE COMMENTATOR HAS LOST IT TOO LADS",
		"genuinely the best touch i've seen all season and i watch far too much football",
		"woke the baby. no regrets",
		"my downstairs neighbour now knows exactly which team i support",
		"GOOO",
		"that's the kind of goal you ring your dad about before it's even finished",
		"knee slide across the living room, took the lamp with me, worth every penny",
		"PUT HIM ON THE PLANE",
		"stream lagged so i found out from next door screaming first, still buzzing",
		"WHAT A HIT, surely goal of the season",
		"i will be telling my grandchildren about this exact moment",
		"framing this. printing it out, framing it, hanging it in the hallway",
		"unreal",
		"the roof has come clean off this place",
	)
	demoAftermath = newPool(
		"keeper had no chance, don't even @ him",
		"replay it. again. and again. one more time",
		"that's the game right there, park the bus now",
		"tactical masterclass from a man who was asleep til now",
		"told you he'd come good and you all doubted me smh",
		"vindicated",
		"not saying i called it but please do check my comment history from twenty minutes ago",
	)
	demoLull = newPool(
		"sub him off he's playing in oven gloves",
		"need to see this out without losing my remaining hair",
		"bring on fresh legs, these ones have clocked off",
		"park the bus, i'm begging on both knees",
		"momentum's flatter than my mate's predictions",
		"zzz",
		"we've gone from end-to-end thriller to two teams politely passing it sideways for a laugh",
	)
	demoVAR = newPool(
		"VAR check incoming, everyone hold your breath",
		"ref's gone to the little telly, here we go",
		"is that a pen?? i refuse to look",
		"drawing the lines... i've aged again waiting",
		"this check is taking longer than my last relationship",
		"wait",
		"they've stared at that monitor so long i made a tea, came back, and they're still looking",
	)
	demoRefRage = newPool(
		"how was that NOT a card",
		"this ref's having a shocker, someone get him a guide dog",
		"yellow all day, my nan saw that from the kitchen",
		"offside by a single nostril hair i swear down",
		"never a foul, this man is allergic to consistency",
		"we've been robbed, i'm calling the actual police",
		"book him for THAT but not the karate kick earlier, ok",
		"i've seen better officiating in a pub car park",
		"VAR's sponsored by the other lot i swear",
		"that's a red, a sending off, AND a prison sentence",
		"VILE",
		"i have watched that back eleven times and i am angrier on each individual viewing somehow",
	)
	demoLate = newPool(
		"5 added on?? where, in which dimension",
		"pack it up lads it's over, i can feel it",
		"SEE IT OUT i cannot do extra time tonight",
		"my heart rate is a genuine war crime right now",
		"BLOW THE WHISTLE ref i am begging you",
		"end it",
		"if they concede in stoppage time i am personally driving to the ground to have a word",
	)
)

// demoScene is one beat of the arc: a themed pool, how many comments to drop,
// the pace until the next beat, and whether it's a "hot" moment (a rush that
// gets piled with upvotes, like a goal or a contentious call).
type demoScene struct {
	pool  *demoPool
	count int
	gap   time.Duration
	hot   bool
}

// demoArc is one loop of a match: calm chatter, a chance building, a GOAL rush,
// the aftermath, a lull, a VAR check, a refereeing pile-on, then late drama —
// then it repeats. The rhythm (fast bursts vs. slow lulls) lives here alongside
// the content, so the two always agree.
var demoArc = []demoScene{
	{demoChatter, 1, 5 * time.Second, false},
	{demoChatter, 1, 6 * time.Second, false},
	{demoBuildUp, 2, 2500 * time.Millisecond, false},
	{demoBuildUp, 1, 1500 * time.Millisecond, false},
	{demoGoal, 9, 450 * time.Millisecond, true}, // GOAL — the eruption
	{demoGoal, 8, 550 * time.Millisecond, true},
	{demoGoal, 6, 800 * time.Millisecond, true}, // still bouncing
	{demoAftermath, 3, 3 * time.Second, false},
	{demoAftermath, 1, 5 * time.Second, false},
	{demoLull, 1, 7 * time.Second, false}, // it settles
	{demoLull, 1, 6 * time.Second, false},
	{demoVAR, 2, 2 * time.Second, false},           // something brewing
	{demoRefRage, 5, 600 * time.Millisecond, true}, // pile-on
	{demoRefRage, 4, 900 * time.Millisecond, true},
	{demoLull, 1, 6 * time.Second, false},
	{demoLate, 2, 3 * time.Second, false}, // stoppage-time drama
	{demoLate, 1, 5 * time.Second, false},
}

// demoPost fabricates an OP so the OP modal works in demo mode (no network).
// The body is markdown so it exercises glamour rendering (headers, tables…).
func demoPost(title string) *post {
	body := "## Lineups are in\n\n" +
		"Kickoff in 10. The bench looks strong today.\n\n" +
		"| Info | Details |\n" +
		"| --- | --- |\n" +
		"| Competition | Demo League |\n" +
		"| Venue | The Terminal, localhost |\n" +
		"| Kickoff | in 10 minutes |\n\n" +
		"**Drop your score predictions below** — last week's thread aged like milk.\n\n" +
		"Usual rules: no streams, keep it civil, and enjoy the match. ⚽"
	return &post{
		title:    title,
		author:   "the_gaffer",
		body:     body,
		score:    1287,
		hasScore: true,
		postedAt: time.Now().Add(-37 * time.Minute),
	}
}

// demoComment builds the i-th fake comment from a given body. hot moments (a
// goal, a contentious call) get piled with upvotes; ordinary chatter gets a
// normal spread including the odd downvoted take.
func demoComment(i int, body string, hot bool) comment {
	score := (i*7)%120 - 20
	if hot {
		score = 60 + (i*23)%180
	}
	return comment{
		id:       fmt.Sprintf("demo-%d", i),
		author:   sampleAuthors[i%len(sampleAuthors)],
		body:     body,
		score:    score,
		hasScore: true,
		postedAt: time.Now(),
	}
}
