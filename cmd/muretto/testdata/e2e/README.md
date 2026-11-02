# End-to-end fixtures

Fake provider responses for `cmd/muretto/e2e_test.go`, in the documented
shapes from `adapters/supabase/research.md` and `adapters/vercel/research.md`. Nothing
here was recorded from a real account. The fake values follow
`adapters/FIXTURES.md` (project ref `abcdefghijklmnopqrst`, org
`example-org`, team `team_EXAMPLE00000000000000` / `example-team`).

Times are placeholders filled in when the test serves the file, relative to the
test's start: `{{ts:-115m}}` is an RFC 3339 time 115 minutes earlier and
`{{ms:-115m}}` the same instant in epoch milliseconds. The routes that serve
each file are in the test.
