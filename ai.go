package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const openAIModel = "gpt-4o-mini"

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature float64      `json:"temperature"`
}

type oaiResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// chatComplete runs one chat completion and returns the trimmed reply.
func chatComplete(system, user string, maxTokens int, temp float64) (string, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return "", fmt.Errorf("set OPENAI_API_KEY in .env")
	}

	reqBody, err := json.Marshal(oaiRequest{
		Model:       openAIModel,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Messages: []oaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach OpenAI: %w", err)
	}
	defer resp.Body.Close()

	var out oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		// A non-JSON body (5xx HTML, a proxy page) is clearer as its status.
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("openai: %s", resp.Status)
		}
		return "", fmt.Errorf("openai: unreadable response: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("openai: %s", out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: %s", resp.Status)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// threadCorpus formats the subreddit/title context, the original post (pinned at
// the top so the model knows what's being discussed), plus comment bodies.
func threadCorpus(sub, title, opBody string, bodies []string) string {
	var b strings.Builder
	if sub != "" {
		b.WriteString("Subreddit: " + sub + "\n")
	}
	if title != "" {
		b.WriteString("Thread: " + title + "\n")
	}
	if op := strings.TrimSpace(opBody); op != "" {
		// Generous cap: match-thread OPs carry lineups, stats, and a live event
		// log that grows during the match — keep enough to include late goals/subs.
		if len(op) > 4000 {
			op = op[:4000]
		}
		b.WriteString("\nThe original post everyone is replying to (the OP — this is the post itself, not a comment):\n")
		b.WriteString(op)
		b.WriteString("\n")
	}
	b.WriteString("\nRecent comments (oldest first):\n")
	for _, body := range bodies {
		body = strings.TrimSpace(body)
		if len(body) > 400 {
			body = body[:400]
		}
		b.WriteString("- ")
		b.WriteString(body)
		b.WriteByte('\n')
		if b.Len() > 16000 { // generous but still cheap on gpt-4o-mini
			break
		}
	}
	return b.String()
}

const sentimentSystemPrompt = `You're a sharp, funny redditor giving a one-line read on what's happening in a live thread RIGHT NOW.

Capture the ACTUAL specifics: the hot takes, the running jokes, the arguments, who's getting roasted, the thing everyone suddenly cares about. Name names and reference what people are literally saying. Match the vibe of the subreddit — casual, dry, a little unhinged (lowercase is fine, swearing is fine if the thread is like that).

Hard rules:
- ONE line, ~20 words max.
- Focus on what the COMMENTS are saying RIGHT NOW. The OP is only context — it tells you the topic, score, and lineups so you know what they might be talking about. Do NOT summarize the OP; summarize the conversation.
- NO "the mood is…", NO "users are…", NO "overall", NO hedging, NO preamble, NO quotes.
- Don't describe the sentiment in the abstract — say the actual thing that's going on.
- VARY your opening every time. Do NOT start with "everyone" or "half the thread". Lead with a player, a specific take, a name, a joke — whatever the thread is actually about.

Bad (generic): "The mood is chaotic but entertained, mixing frustration and excitement."

Good (notice the different openings):
- "Diomande's getting man-of-the-match shouts while the ref gets absolutely cooked over that offside call"
- "VAR check has the thread holding its breath and someone's already blaming FIFA"
- "Curacao slander in full swing, a few neutrals just here for the empty-stadium jokes"
- "scoreless but nobody cares, the banter about the BBC pundits is carrying this thread"`

// summarizeSentiment asks the model for a one-line, reddit-flavored read on the
// thread, grounded with the subreddit + title + original post for context.
func summarizeSentiment(sub, title, opBody string, bodies []string) (string, error) {
	if len(bodies) == 0 {
		return "", fmt.Errorf("no comments to summarize yet")
	}
	return chatComplete(sentimentSystemPrompt, threadCorpus(sub, title, opBody, bodies), 80, 0.85)
}

const askSystemPrompt = `You're a chill, funny redditor answering a question about a live thread, using ONLY the original post (OP) and the comments provided. You're reporting what the post says and what the thread is saying — NOT stating outside facts.

Voice: casual, dry, a little irreverent (lowercase is fine, swearing is fine if the thread is like that).

Grounding rules (these override the vibe — never break them):
- NEVER make up info or use outside knowledge. If it's not in the OP or the comments, you don't know it.
- The OP is the post itself — you can treat what it states (score, lineups, the topic) as the thread's framing. Comments are opinions: commenters lie, joke, and get it wrong, so attribute those ("people are saying…", "a few reckon…", "thread's convinced…").
- If neither the OP nor the comments cover it, just say so ("nobody's brought that up", "thread's no help there").

ONE line, ~25 words max. No preamble, no quotes.

Examples (grounded but chill):
- "people are adamant it was offside, replays apparently back them up but the ref's not having it"
- "thread reckons Havertz in the 88th, though a couple are crediting the keeper's howler"
- "lol nobody's actually answered that, they're too busy roasting the commentary"
- "no clue from the comments, half of them are just posting popcorn gifs"`

// askThread answers a user's question grounded in the thread's comments. prior
// holds the user's earlier questions this session, kept clearly separate so the
// model doesn't mistake them for thread comments.
func askThread(sub, title, opBody, question string, bodies, prior []string) (string, error) {
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("ask a question first")
	}
	if len(bodies) == 0 {
		return "", fmt.Errorf("no comments to read yet")
	}

	var u strings.Builder
	u.WriteString(threadCorpus(sub, title, opBody, bodies))
	if len(prior) > 0 {
		u.WriteString("\nYOUR earlier questions to me this session (from the app user, NOT thread comments — context for follow-ups only):\n")
		for _, p := range prior {
			u.WriteString("- " + p + "\n")
		}
	}
	u.WriteString("\nNow answer this question: " + strings.TrimSpace(question))
	return chatComplete(askSystemPrompt, u.String(), 120, 0.7)
}
