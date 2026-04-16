# Test Guidelines

## Overview

All tests are **integration tests** that run the real `proton-cli` binary against the live Proton API. There are no unit tests or mocks — every test creates real data, verifies it, and cleans it up.

## Running Tests

```bash
# All tests (requires credentials in env)
just test

# Single test
just test-one TestDriveMv
```

Tests skip automatically if `PROTON_USER` and `PROTON_PASSWORD` are not set.

## How Tests Work

1. `TestMain` in `integration_test.go` builds the binary once into a temp directory
2. Each test calls the binary as a subprocess via `run()` / `runOK()` / `runJSON()`
3. The binary picks up `PROTON_USER` and `PROTON_PASSWORD` from the environment
4. Tests are sequential (no parallelism) to avoid API rate limits and shared state conflicts

## Writing a Test

Follow the **Arrange → Act → Assert** pattern. Every test that creates data must register cleanup.

```go
func TestDriveExample(t *testing.T) {
    skipIfNoCredentials(t)

    // Arrange: create test data
    folder := testID() + "-example"
    runOK(t, "drive", "mkdir", "/"+folder)
    cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
        "drive", "rm", "/"+folder)

    // Act: run the command being tested
    stdout := runOK(t, "drive", "ls", "/"+folder)

    // Assert: verify the output
    assertContains(t, stdout, "something-expected")
}
```

## Arrange Creates What the Test Needs

If a command needs something to exist first, the **Arrange** step creates it. Never rely on pre-existing data in the account.

- Testing `drive download`? Upload a file first.
- Testing `drive mv`? Create two folders and upload a file.
- Testing `mail mark`? Send a mail to self first via `sendTestMail()`.
- Testing `contacts update`? Create a contact first.

Every piece of test data created in Arrange must have a matching cleanup registered **immediately after creation**, before the Act step. This way, if the test fails during Act or Assert, the data still gets cleaned up.

```go
func TestDriveDownloadToFile(t *testing.T) {
    skipIfNoCredentials(t)

    // Arrange: create folder + upload file
    folder := testID() + "-download"
    runOK(t, "drive", "mkdir", "/"+folder)
    cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
        "drive", "rm", "/"+folder)

    tmpFile := filepath.Join(t.TempDir(), "source.txt")
    _ = os.WriteFile(tmpFile, []byte("test content"), 0644)
    runOK(t, "drive", "upload", tmpFile, "/"+folder)

    // Act: download the file we just uploaded
    outPath := filepath.Join(t.TempDir(), "output.txt")
    runOK(t, "drive", "download", "/"+folder+"/source.txt", outPath)

    // Assert: content matches
    data, _ := os.ReadFile(outPath)
    if string(data) != "test content" {
        t.Errorf("content mismatch")
    }
}
```

## Cleanup Rules

- **Always register cleanup**, even if the test is about deletion — the test might fail before reaching the delete step
- Use `cleanupRun()` for CLI commands or `cleanup()` for custom functions
- Cleanup runs even on test failure (`t.Cleanup()` guarantees this)
- If cleanup fails, a loud box is printed with the exact manual command to run:

```
╔══════════════════════════════════════════════════════════════╗
║  ⚠️  CLEANUP FAILED — MANUAL ACTION REQUIRED                ║
╠══════════════════════════════════════════════════════════════╣
║  Delete folder: proton-cli drive rm /test-folder-xyz
║  Error: exit 1: ...
╚══════════════════════════════════════════════════════════════╝
```

- The description in `cleanupRun()` must be a copy-pasteable command so the user can fix it manually

## Helpers

| Helper | Purpose |
|---|---|
| `skipIfNoCredentials(t)` | Skip test if env vars not set |
| `run(t, args...)` | Run binary, return stdout/stderr/exitCode |
| `runOK(t, args...)` | Run binary, fail test if exit != 0, return stdout |
| `runJSON(t, args...)` | Run with `--json`, parse as `map[string]interface{}` |
| `runJSONArray(t, args...)` | Run with `--json`, parse as `[]interface{}` |
| `testID()` | Generate unique `proton-cli-test-{timestamp}-{random}` prefix |
| `cleanupRun(t, description, args...)` | Register cleanup that runs a CLI command |
| `cleanup(t, description, func() error)` | Register cleanup with custom function |
| `assertContains(t, stdout, substr)` | Assert stdout contains substring |
| `assertNotContains(t, stdout, substr)` | Assert stdout does not contain substring |
| `jsonField(data, "path", "to", "field")` | Extract string from nested JSON map |
| `sendTestMail(t, subject)` | Send a mail to self, register cleanup, return inbox message ID |
| `selfEmail()` | Return `PROTON_USER` value |

## Naming Conventions

- Test artifacts use `proton-cli-test-{timestamp}-{random}-{purpose}` names (via `testID()`)
- This makes them identifiable in the Proton UI if cleanup fails
- Never use short or common names that could collide with real data

## Cobra and Message IDs

Proton message IDs can start with `-` which cobra interprets as flags. Always use `--` before positional ID arguments:

```go
// Wrong — breaks if ID starts with -
runOK(t, "mail", "mark", "--read", msgID)

// Correct
runOK(t, "mail", "mark", "--read", "--", msgID)
```

This applies to: `mail mark`, `mail trash`, `mail delete`, `mail move`.

## Extracting IDs from Create Responses

When a test creates something, extract the ID from the JSON response for cleanup:

```go
// Contacts: ID is nested in Responses[0].Response.Contact.ID
stdout := runOK(t, "contacts", "create", "--name", name, "--email", email)
var result map[string]interface{}
json.Unmarshal([]byte(stdout), &result)
responses := result["Responses"].([]interface{})
resp := responses[0].(map[string]interface{})
contactID := jsonField(resp["Response"].(map[string]interface{}), "Contact", "ID")

// Labels: ID is in Label.ID
stdout := runOK(t, "mail", "labels", "create", "--name", name, "--color", "#8080FF")
var result map[string]interface{}
json.Unmarshal([]byte(stdout), &result)
labelID := jsonField(result, "Label", "ID")
```

## Known Limitations

- `calendar delete` requires `PROTON_PASSWORD` for scope unlock — this works in tests since the env var is set
- `drive trash empty` may not clear items from all volume types (photos share)
- Proton only allows specific hex colors for labels and calendars (e.g. `#8080FF`, `#3CBB3A`) — see `ACCENT_COLORS` in WebClients source
- Tests run ~6 minutes total due to API latency and mail delivery waits
