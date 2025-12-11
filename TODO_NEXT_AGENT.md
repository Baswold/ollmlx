# TODO for Next Agent - ollmlx Project

**Last Updated:** 2025-12-10 (Post-Codex Session)
**Project Status:** 90% complete, experimental features added, critical bugs identified
**Priority Order:** Critical bugs → Testing → Cleanup → Optional enhancements

---

## 🎯 Mission Overview

This project (Ollama-mlx_claude) is a merged version combining:
- **Infrastructure** from `Ollama-MLX_small_model` (excellent docs, tests, build)
- **Working MLX backend** from `Ollama-MLX_big_model` (476 lines → now 554 lines)
- **Recent additions** from Codex (tool-calling, fine-tuning, install script)

**Current State:**
- ✅ Core MLX generation works
- ✅ Builds successfully
- ⚠️ Experimental features added (tool-calling, fine-tuning)
- 🔴 3 critical bugs identified
- 📋 50+ TODOs in codebase

**Your Goal:** Fix critical issues, validate features, prepare for v1.0 release.

---

## 🔴 PHASE 1: Fix Critical Bugs (HIGH PRIORITY)

These bugs can cause server crashes or silent failures. **Do these first.**

### Task 1.1: Fix Server Crash in images.go

**Problem:** Server crashes entirely if image digest fails.

**File:** `server/images.go`
**Line:** 746 (approximately)
**Current Code:**
```go
digest, err := GetSHA256Digest(reader)
if err != nil {
    log.Fatal(err)  // ❌ This crashes the entire server!
}
```

**What to do:**
1. Open `server/images.go`
2. Search for `log.Fatal(err)` around line 746
3. Replace with proper error handling:
   ```go
   digest, err := GetSHA256Digest(reader)
   if err != nil {
       slog.Error("failed to get SHA256 digest", "error", err)
       return "", fmt.Errorf("digest calculation failed: %w", err)
   }
   ```

**Why this matters:** `log.Fatal()` terminates the entire process. In a server, errors should be logged and returned, not crash everything.

**How to verify:**
```bash
# After fix, try to trigger the error path
# Server should log error but keep running
grep -n "log.Fatal" server/images.go  # Should show no results near digest code
```

---

### Task 1.2: Fix Ignored Error in MLX Runner

**Problem:** Errors from MLX backend are silently ignored, causing mysterious failures.

**File:** `runner/mlxrunner/runner.go`
**Line:** 158 (approximately)
**Current Code:**
```go
body, _ := io.ReadAll(resp.Body)  // ❌ Error is ignored!
```

**What to do:**
1. Open `runner/mlxrunner/runner.go`
2. Find the line with `body, _ := io.ReadAll(resp.Body)`
3. Replace with proper error handling:
   ```go
   body, err := io.ReadAll(resp.Body)
   if err != nil {
       slog.Error("failed to read MLX backend response", "error", err)
       return fmt.Errorf("failed to read response body: %w", err)
   }
   ```

**Why this matters:** If reading the response fails, you'll never know why MLX isn't working. The `_` discards critical error information.

**How to verify:**
```bash
# Check that error is now handled
grep "_, _ :=" runner/mlxrunner/runner.go  # Should be empty or minimal
grep "err := io.ReadAll" runner/mlxrunner/runner.go  # Should show your fix
```

---

### Task 1.3: Fix Resource Leaks in HTTP Handlers

**Problem:** HTTP response bodies aren't always closed, leading to connection leaks.

**Files to check:**
- `server/images.go` (multiple HTTP calls)
- `runner/mlxrunner/runner.go` (HTTP client code)
- `api/client.go` (API client calls)

**Pattern to look for:**
```go
// ❌ BAD - no cleanup
resp, err := http.Get(url)
// ... use resp.Body ...
// Missing: defer resp.Body.Close()
```

**What to do:**
1. Search for `http.Get`, `http.Post`, `client.Do` in these files
2. For each occurrence, check if there's a `defer resp.Body.Close()` immediately after error check
3. Add it if missing:
   ```go
   // ✅ GOOD - proper cleanup
   resp, err := http.Get(url)
   if err != nil {
       return err
   }
   defer resp.Body.Close()  // Always close!
   ```

**How to verify:**
```bash
# Find all HTTP calls without defer close
grep -n "http\.Get\|http\.Post\|client\.Do" server/images.go runner/mlxrunner/runner.go api/client.go > http_calls.txt
# Manually check each one has defer resp.Body.Close() after it
```

**Pro tip:** The defer should come RIGHT AFTER checking `err != nil`, before any other code.

---

## 🧪 PHASE 2: Test Experimental Features (MEDIUM PRIORITY)

Codex added tool-calling and fine-tuning. We need to verify they work.

### Task 2.1: Test MLX Tool-Calling

**What it is:** Experimental tool-calling support (non-streaming).

**File to understand:** `server/routes_mlx.go` (new file from Codex)

**Test Plan:**

1. **Start the server:**
   ```bash
   ./ollmlx serve
   ```

2. **Pull a compatible model:**
   ```bash
   ./ollmlx pull mlx-community/Llama-3.2-3B-Instruct-4bit
   ```

3. **Test tool-calling via API:**
   ```bash
   curl http://localhost:11434/api/chat -d '{
     "model": "mlx-community/Llama-3.2-3B-Instruct-4bit",
     "messages": [{"role": "user", "content": "What is the weather?"}],
     "tools": [{
       "type": "function",
       "function": {
         "name": "get_weather",
         "description": "Get current weather",
         "parameters": {
           "type": "object",
           "properties": {
             "location": {"type": "string"}
           }
         }
       }
     }]
   }'
   ```

4. **Expected behavior:**
   - Should NOT return 501 error
   - Should return response with `tool_calls` field
   - Response should be non-streaming (all at once)

5. **Document results:**
   Create a file `test_results_tool_calling.md`:
   ```markdown
   # Tool-Calling Test Results

   ## Test 1: Basic tool call
   - Date: [today]
   - Model: mlx-community/Llama-3.2-3B-Instruct-4bit
   - Result: [PASS/FAIL]
   - Notes: [what happened]

   ## Issues found:
   - [list any issues]
   ```

**If it fails:** Check `codex_changes.md` - tool-calling might still be experimental. Update README to clarify status.

---

### Task 2.2: Test Fine-Tuning Endpoint

**What it is:** `/finetune` endpoint that delegates to `mlx_lm.finetune()`.

**File to understand:** `mlx_backend/server.py` lines 481+ (the `/finetune` endpoint)

**Test Plan:**

1. **Check if mlx_lm supports fine-tuning:**
   ```bash
   python3 -c "import mlx_lm; print(hasattr(mlx_lm, 'finetune'))"
   ```

2. **If True, test the endpoint:**
   ```bash
   curl -X POST http://localhost:11434/finetune \
     -H "Content-Type: application/json" \
     -d '{
       "model": "mlx-community/SmolLM2-135M-Instruct-4bit",
       "dataset": "/tmp/test_data.jsonl",
       "output_dir": "/tmp/finetuned",
       "epochs": 1
     }'
   ```

3. **Expected behavior:**
   - If `mlx_lm.finetune` exists: Should start fine-tuning
   - If not: Should return 501 with clear message

4. **Document results:**
   Add to `test_results_finetuning.md`

**If mlx_lm doesn't have finetune:** That's OK! The code handles it gracefully (returns 501). Just document this in README.

---

### Task 2.3: Verify Install Script Works

**File:** `scripts/install_ollmlx.sh`

**Test Plan:**

1. **In a fresh directory:**
   ```bash
   cd /tmp
   cp -r /Users/basil_jackson/Documents/Ollama-mlx_claude ./test-install
   cd test-install
   ```

2. **Run install script:**
   ```bash
   ./scripts/install_ollmlx.sh
   ```

3. **Verify it:**
   - Installs Python dependencies
   - Builds the binary
   - Binary works: `./ollmlx --version`

4. **Document any issues in:** `scripts/install_issues.md`

---

## 🧹 PHASE 3: Code Cleanup (LOW-MEDIUM PRIORITY)

Make the codebase maintainable.

### Task 3.1: Remove Duplicate -lobjc Warning

**Problem:** Build shows `ld: warning: ignoring duplicate libraries: '-lobjc'`

**What to do:**

1. **Find the build configuration:**
   ```bash
   grep -r "\-lobjc" . --include="*.go" --include="Makefile" --include="*.cmake"
   ```

2. **Look for duplicate library flags in:**
   - CGO directives (`#cgo LDFLAGS:`)
   - Build scripts
   - CMakeLists.txt

3. **Fix approach:**
   - Find where `-lobjc` is specified multiple times
   - Remove duplicate (keep one)
   - Or add `-Wl,-no-warn-duplicate-libraries` to suppress

**Note:** This is harmless but noisy. Low priority if time is short.

---

### Task 3.2: Document Current TODO Comments

**What to do:**

1. **Extract all TODOs:**
   ```bash
   grep -r "TODO\|FIXME\|XXX\|HACK" . --include="*.go" --include="*.py" > all_todos.txt
   ```

2. **Categorize them:**
   - Critical (affects functionality)
   - Nice-to-have (improvements)
   - Can-ignore (old comments)

3. **Create TODO_INVENTORY.md:**
   ```markdown
   # TODO Inventory

   ## Critical (Fix Soon)
   - [file:line] description

   ## Nice-to-Have
   - [file:line] description

   ## Can Defer
   - [file:line] description
   ```

**Why:** The codebase has 50+ TODOs. This makes them manageable.

---

### Task 3.3: Update MERGE_SUMMARY.md

**What changed:** Codex added features after the merge.

**What to do:**

1. **Read:** `codex_changes.md` (recent additions)
2. **Update:** `MERGE_SUMMARY.md` to add a new section:

```markdown
## Post-Merge Additions (Codex Session)

### Features Added (2025-12-10):
- Tool-calling support (experimental, non-streaming)
- Fine-tuning endpoint via mlx_lm
- Metal GPU default in MLX backend
- Install script at scripts/install_ollmlx.sh
- Enhanced verbose mode

### Code Changes:
- mlx_backend/server.py: 476 → 554 lines (+16%)
- New file: server/routes_mlx.go
- Updated: 11 core files

### Status:
- Tool-calling: Experimental, non-streaming
- Fine-tuning: Experimental, requires mlx_lm
- Core MLX: Stable ✅
```

---

## 📚 PHASE 4: Documentation Polish (OPTIONAL)

### Task 4.1: Update README.md Status Badges

Add a status section to README after the header:

```markdown
## Project Status

| Component | Status | Notes |
|-----------|--------|-------|
| MLX Generation | ✅ Stable | Core feature working |
| GGUF Support | ✅ Stable | Full Ollama compatibility |
| Tool-Calling | ⚠️ Experimental | Non-streaming only |
| Fine-Tuning | ⚠️ Experimental | Requires mlx_lm |
| Build | ✅ Passing | Minor warnings |
```

**Why:** Sets clear expectations for users.

---

### Task 4.2: Create TESTING_GUIDE.md

**Purpose:** Help users/developers test the project.

**Content:**
```markdown
# Testing Guide

## Quick Smoke Test (5 minutes)

1. Build: `go build -o ollmlx .`
2. Start: `./ollmlx serve`
3. Pull: `./ollmlx pull mlx-community/gemma-3-270m-4bit`
4. Run: `echo "Hello" | ./ollmlx run mlx-community/gemma-3-270m-4bit`
5. ✅ If you get a response, core functionality works!

## Full Test Suite (30 minutes)

[Detailed test procedures for each feature]
```

---

### Task 4.3: Simplify QUICKSTART.md

**Current issue:** QUICKSTART is very long.

**What to do:**

1. Read current QUICKSTART.md
2. Create QUICKSTART_SIMPLE.md with just:
   - Install (3 commands)
   - Pull model (1 command)
   - Run model (1 command)
   - That's it!

Keep current QUICKSTART.md as QUICKSTART_DETAILED.md.

---

## 🎁 PHASE 5: Bonus Improvements (NICE-TO-HAVE)

### Task 5.1: Add Homebrew Install Option

**What:** Make it installable via `brew install ollmlx`

**Steps:**
1. Create a Homebrew formula (`.rb` file)
2. Host it in a tap repository
3. Document in README

**Note:** This is advanced. Skip if you're not familiar with Homebrew.

---

### Task 5.2: Performance Benchmarking

**Purpose:** Validate "2-3x faster" claims in README.

**What to do:**

1. **Create benchmark script:**
   ```bash
   # test_performance.sh
   # Measures tokens/second for GGUF vs MLX
   ```

2. **Test with same model in both formats:**
   - GGUF: via regular Ollama
   - MLX: via ollmlx

3. **Document results in:** `PERFORMANCE_RESULTS.md`

**Models to test:**
- Small: gemma-270m (fast baseline)
- Medium: Llama-3.2-3B (realistic use case)

---

### Task 5.3: CI/CD Pipeline

**Goal:** Automatic testing on every commit.

**What to do:**

1. Create `.github/workflows/test.yml`:
   ```yaml
   name: Test ollmlx
   on: [push, pull_request]
   jobs:
     test:
       runs-on: macos-latest
       steps:
         - uses: actions/checkout@v3
         - run: go test ./...
         - run: go build .
   ```

2. Enable GitHub Actions on the repo

**Why:** Catches regressions automatically.

---

## 📋 Checklist for Next Agent

Before you start, verify you have:
- [ ] Read this entire file
- [ ] Located all files mentioned
- [ ] Go 1.21+ installed
- [ ] Python 3.10+ installed
- [ ] Write access to the repository

As you work:
- [ ] Use TodoWrite tool to track progress
- [ ] Document what you find
- [ ] Create test result files
- [ ] Update CHANGELOG_OLLMLX.md

When you're done:
- [ ] All critical bugs fixed
- [ ] Test results documented
- [ ] README updated with current status
- [ ] Code builds without errors

---

## 🆘 If You Get Stuck

### Problem: Can't find a file mentioned
**Solution:** Run `find . -name "filename"` to locate it

### Problem: Don't understand Go/Python code
**Solution:** Focus on the mechanical changes described. You don't need to understand the algorithm, just change the error handling pattern.

### Problem: Tests fail after your changes
**Solution:**
1. Read the error message carefully
2. Check you didn't introduce syntax errors
3. Revert your change: `git checkout -- filename`
4. Try again more carefully

### Problem: Build fails
**Solution:**
```bash
# Check syntax
go build ./...

# See specific error
go build -v .

# Clean and rebuild
go clean && go build .
```

---

## 📊 Success Metrics

You'll know you succeeded when:

1. **Critical bugs fixed:**
   - No more `log.Fatal` in image handling
   - No more ignored errors in MLX runner
   - All HTTP responses properly closed

2. **Tests documented:**
   - Tool-calling tested (results in `test_results_tool_calling.md`)
   - Fine-tuning tested (results in `test_results_finetuning.md`)
   - Install script verified

3. **Code clean:**
   - Build succeeds
   - No new warnings introduced
   - CHANGELOG updated

4. **Documentation current:**
   - README reflects actual status
   - Experimental features clearly marked
   - Next steps documented

---

## 🎯 Final Notes

**Time Estimates:**
- Phase 1 (Critical bugs): 2-3 hours
- Phase 2 (Testing): 1-2 hours
- Phase 3 (Cleanup): 1-2 hours
- Phase 4 (Docs): 1 hour
- Phase 5 (Bonus): 3+ hours

**Priority if time is limited:**
- MUST DO: Phase 1 (critical bugs)
- SHOULD DO: Phase 2 (testing)
- NICE TO HAVE: Phases 3-5

**Remember:**
- Test after each change
- Commit working code frequently
- Document what you learn
- Ask questions if unclear

Good luck! You've got this! 🚀

---

**End of TODO_NEXT_AGENT.md**
