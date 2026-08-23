# leetbot

[Português](README.md) | [English](README.en.md)

LeetCode bot written in Go, with no external dependencies. It follows the
architecture described in [*Solving 1,782 Leetcode questions in one day*](https://matthewtrent.me/articles/leetcode-bot): instead of generating code with an LLM, it **collects the community's most-voted solutions**, validates them against the example cases, and only then submits them.

```
problem index -> description + stub -> top-voted solutions
                                             |
                                  extracts code blocks
                                             |
                       interpret_solution (examples) --failed--> next candidate
                                             | passed
                                           submit
                                             |
                              poll /submissions/detail/{id}/check/
```

Validating examples before submitting is what makes the difference: the
original article's author reports that this increased the success rate from
under 50% to about 95%.

## Warning

Automated bulk submissions violate LeetCode's Terms of Service and may get the
account banned. **The default is dry-run**: without `-submit`, nothing is sent;
the bot only runs against the example cases. Consider using a secondary account.

## Installation

### User (public repository)

Requires [Go 1.26+](https://go.dev/dl/). Install the module directly, without
cloning this repository:

```bash
go install github.com/levyvix/leetbot@latest
```

Go installs the executable in `GOBIN` or, by default, in `$(go env GOPATH)/bin`.
That directory must be in your `PATH`. On Linux/macOS, for example:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
leetbot --help
```

Run the same command again to update an existing installation.

This command requires `github.com/levyvix/leetbot` to be public.

### Contributor

To build from a local checkout:

```bash
go build -o leetbot .
```

Then use `./leetbot` in the commands below. The `go install` workflow has no
third-party dependencies.

## Authentication

There is no programmatic login (Cloudflare + captcha). Copy the two cookies
from a browser that is already logged in:

1. Open `https://leetcode.com` while logged in
2. DevTools (F12) → Application → Cookies → `https://leetcode.com`
3. Copy the values of `LEETCODE_SESSION` and `csrftoken`

```bash
export LEETCODE_SESSION='eyJ...'
export LEETCODE_CSRF='abc...'

leetbot whoami
# authenticated as levy_vix (premium: false)
```

`LEETCODE_SESSION` is a JWT valid for about two weeks. When `whoami` reports a
session error, repeat the steps above.

## Usage proof

Example of the LeetCode profile used during development:

![LeetCode profile](docs/leetcode-profile.png)

## Usage

### Show a problem description

Accepts the ID shown in the UI or the slug:

```bash
leetbot show 1
leetbot show two-sum -lang python3
```

### Inspect candidate solutions

Only runs the harvesting stage; it does not execute or submit anything:

```bash
leetbot harvest two-sum -lang python3
leetbot harvest two-sum -lang python3 -articles 1 -print   # print code
```

### Solve a problem

```bash
# dry-run: harvests solutions and tests the examples, without submitting
leetbot solve 1 -lang python3 -print

# actually submits
leetbot solve 1 -lang python3 -submit
```

### Run in batches

```bash
# 20 easy problems, dry-run
leetbot run -difficulty easy -limit 20

# submit slowly
leetbot run -difficulty easy -limit 50 -submit -pause 10s

# resume where it stopped
leetbot run -difficulty easy -limit 200 -submit

# retry only problems that ended as failed
leetbot run -retry-failed -limit 0 -submit
```

Progress is written to `state.json` after **each** problem, so `Ctrl-C` is safe.
The next run skips already processed problems, except those that ended in
`error`, which are retried.

```bash
leetbot stats
# total=20 accepted=17 failed=2 skipped=1
```

### Main `run` flags

| Flag | Default | Description |
| --- | --- | --- |
| `-lang` | `python3` | language slug (`java`, `cpp`, `golang`, `rust`, …) |
| `-submit` | `false` | actually submit |
| `-difficulty` | all | `easy`, `medium`, `hard` |
| `-limit` | `10` | maximum problems in this run (`0` = all) |
| `-from` | `0` | start at this ID |
| `-articles` | `8` | solution posts to read per problem |
| `-candidates` | `5` | code blocks to test per problem |
| `-pause` | `5s` | pause between problems |
| `-interval` | `500ms` | minimum interval between HTTP requests |
| `-include-solved` | `false` | do not skip problems already solved in the account |
| `-retry-failed` | `false` | retry only problems saved as `failed` |

## Possible states

| Status | Meaning |
| --- | --- |
| `accepted` | submitted and accepted |
| `tested` | passed examples, not submitted (dry-run) |
| `failed` | no candidate worked |
| `skipped` | premium, SQL/shell/concurrency, or no stub in the selected language |
| `error` | network or API failure; will be retried |

## Known limitations

- SQL, shell, and concurrency problems are skipped.
- Premium problems are skipped; without a subscription their descriptions are empty.
- A problem without a community solution in the selected language becomes `failed`.
  Try another language (`python3` and `java` have the broadest coverage).
- Extraction keeps Markdown code blocks containing the method name from the
  `metaData`. Prose-only posts and partial snippets are discarded during testing.
- Article bodies use two different encodings; the extractor normalizes both.

## Underlying API

Everything is undocumented and subject to change without notice.

| Operation | Endpoint |
| --- | --- |
| Problem index | `GET /api/problems/all/` |
| Description | `POST /graphql` — `questionData(titleSlug)` |
| Community solutions | `POST /graphql` — `ugcArticleSolutionArticles(questionSlug, orderBy: MOST_VOTES)` |
| Solution content | `POST /graphql` — `ugcArticleSolutionArticle(topicId)` |
| Run without submitting | `POST /problems/{slug}/interpret_solution/` |
| Submit | `POST /problems/{slug}/submit/` |
| Check result | `GET /submissions/detail/{id}/check/` |

REST endpoints require `Referer: https://leetcode.com/problems/{slug}/` and the
`x-csrftoken` header; without them the response is 403.
