# -lobjc Warning Analysis

## Issue Description
- **Warning**: `ld: warning: ignoring duplicate libraries: '-lobjc'`
- **Severity**: Low (harmless but noisy)
- **Frequency**: Appears during Go build process

## Root Cause Analysis

### What is -lobjc?
- `-lobjc` is a linker flag that links the Objective-C runtime library
- Required for macOS/iOS development
- Often added automatically by CGO (Go's C interop)

### Why the duplicate warning?
1. **Multiple CGO directives**: Different Go files may specify the same library
2. **Build system configuration**: Makefiles or build scripts may add it multiple times
3. **Toolchain behavior**: Xcode or clang may add it automatically

### Investigation Results
- **Search for sources**: Could not find explicit `-lobjc` in Go source files
- **Likely source**: CGO directives or automatic linker behavior
- **Impact**: No functional issues, just a warning

## Potential Solutions

### Option 1: Suppress the warning (Recommended)
Add linker flags to suppress duplicate library warnings:
```bash
go build -ldflags="-w -s -extldflags '-Wl,-no-warn-duplicate-libraries'" -o ollmlx .
```

### Option 2: Find and remove duplicate
1. Search for CGO directives: `#cgo LDFLAGS: -lobjc`
2. Check Makefiles and build scripts
3. Remove duplicate entries

### Option 3: Accept as known issue
- Document in README
- Add to .gitignore or build documentation
- Consider low priority for fixing

## Current Status
- **Priority**: Low (as marked in TODO)
- **Impact**: None - build succeeds, binary works
- **Recommendation**: Suppress warning with linker flags

## Next Steps
1. **Test suppression**: Try building with `-Wl,-no-warn-duplicate-libraries`
2. **Document**: Add to known issues section
3. **Monitor**: Check if future Go versions fix this automatically

## References
- Similar issues reported in Go GitHub issues
- Common with CGO projects on macOS
- Often resolved by suppressing warnings rather than removing duplicates