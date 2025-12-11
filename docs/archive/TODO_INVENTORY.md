# TODO Inventory

## Summary

- **Total TODOs found**: 126
- **Critical**: 1
- **Nice-to-Have**: 8
- **Can Defer**: 21
- **Unknown Priority**: 96

## Critical (1)

### ./server/routes_list_test.go

**Type**: TODO

**TODO**: : host:port currently fails on windows (#4107)

---

## Nice_to_have (8)

### ./app/ui/ui.go

**Type**: TODO

**TODO**: (jmorganca): this should be merged with code above

---

### ./app/ui/ui.go

**Type**: TODO

**TODO**: (parthsareen): consolidate events within the function

---

### ./app/cmd/app/app_darwin.go

**Type**: TODO

**TODO**: (jmorganca): find a better check for development mode than checking the bundle path

---

### ./app/updater/updater_darwin.go

**Type**: TODO

**TODO**: handle failure modes here, and developer mode better...

---

### ./server/sched.go

**Type**: TODO

**TODO**: consolidate sched_types.go

---

### ./parser/parser.go

**Type**: TODO

**TODO**: (mxyng): merge this with the glob above

---

### ./ml/device.go

**Type**: TODO

**TODO**: in the future if we find Vulkan is better than ROCm on some devices

---

### ./model/wordpiece.go

**Type**: TODO

**TODO**: : this is specifically for BERT and may need to be adjusted or refactored for other models.

---

## Can_defer (21)

### ./discover/runner.go

**Type**: TODO

**TODO**: - consider evaluating if new devices have appeared (e.g. hotplug)

---

### ./app/cmd/app/app_darwin.go

**Type**: TODO

**TODO**: - consider a timer that aborts if this takes too long and we haven't been killed yet...

---

### ./app/cmd/app/app.go

**Type**: TODO

**TODO**: - consider a timer that aborts if this takes too long and we haven't been killed yet...

---

### ./app/cmd/app/webview.go

**Type**: TODO

**TODO**: (jmorganca): later we should use proper accelerators

---

### ./app/server/server.go

**Type**: TODO

**TODO**: consider rotation based on size or time, not just every server invocation

---

### ./app/updater/updater.go

**Type**: TODO

**TODO**: - maybe move up to the API package?

---

### ./app/updater/updater_windows.go

**Type**: TODO

**TODO**: - some details about why it didn't start, or is this a pedantic error case?

---

### ./app/updater/updater_darwin.go

**Type**: TODO

**TODO**: - in the future, consider shutting down the backend server now to give it

---

### ./app/store/database.go

**Type**: TODO

**TODO**: : Can eventually be removed - cleans up data from foreign key bug (ollama/ollama#11785, ollama/app#476)

---

### ./integration/concurrency_test.go

**Type**: TODO

**TODO**: consider retrying the medium models

---

### ./integration/utils_test.go

**Type**: TODO

**TODO**: use info API in the future

---

### ./integration/model_arch_test.go

**Type**: TODO

**TODO**: use info API eventually

---

### ./integration/model_arch_test.go

**Type**: TODO

**TODO**: use info API eventually

---

### ./integration/model_perf_test.go

**Type**: TODO

**TODO**: use info API eventually

---

### ./server/internal/registry/server.go

**Type**: TODO

**TODO**: (bmizerany): Decide if we want to keep this and maybe

---

### ./server/internal/client/ollama/registry.go

**Type**: TODO

**TODO**: (bmizerany): decide if this should be considered valid. Maybe

---

### ./server/images.go

**Type**: TODO

**TODO**: : remove this warning in a future version

---

### ./server/sched.go

**Type**: TODO

**TODO**: maybe we should just always trust our numbers, since cuda's free memory reporting is laggy

---

### ./server/sched.go

**Type**: TODO

**TODO**: - future consideration to pick runners based on size

---

### ./server/routes.go

**Type**: TODO

**TODO**: (parthsareen): consider adding prefill disambiguation logic to the renderer for structured outputs.

---

### ./fs/ggml/gguf.go

**Type**: TODO

**TODO**: consider reducing if tensors size * gomaxprocs is larger than free memory

---

## Unknown (96)

### ./cmd/cmd.go

**Type**: TODO

**TODO**: : this is incorrect since the file might be in a subdirectory

---

### ./cmd/cmd.go

**Type**: TODO

**TODO**: : same here

---

### ./cmd/cmd.go

**Type**: TODO

**TODO**: : remove the projector info and vision info checks below,

---

### ./cmd/interactive.go

**Type**: TODO

**TODO**: (drifkin): validate the level, could be model dependent

---

### ./tools/tools.go

**Type**: TODO

**TODO**: (jmorganca): this does not support parsing omitted arguments

---

### ./types/errtypes/errtypes.go

**Type**: TODO

**TODO**: : This should have a structured response from the API

---

### ./llm/server.go

**Type**: TODO

**TODO**: - NUMA support currently doesn't work properly

---

### ./llm/status.go

**Type**: TODO

**TODO**: - regex matching to detect errors like

---

### ./app/ui/ui.go

**Type**: TODO

**TODO**: : this avoids an error on first load of the app

---

### ./app/ui/ui.go

**Type**: TODO

**TODO**: (jmorganca): skip this round trip and instead just act

---

### ./app/ui/ui.go

**Type**: TODO

**TODO**: (jmorganca): this only shows the largest digest, but we

---

### ./app/ui/ui.go

**Type**: TODO

**TODO**: (parthsareen): this logic will change with directory drag and drop

---

### ./app/cmd/app/app_darwin.go

**Type**: TODO

**TODO**: (jmorganca): pre-create the window and pass

---

### ./app/cmd/app/app_windows.go

**Type**: TODO

**TODO**: - reconcile with above for consistency between mac/windows

---

### ./app/cmd/app/app_windows.go

**Type**: TODO

**TODO**: - can this be generalized?

---

### ./app/cmd/app/app.go

**Type**: TODO

**TODO**: (jmorganca): instead we should instantiate the

---

### ./app/cmd/app/webview.go

**Type**: TODO

**TODO**: (jmorganca): we should pre-create the window and then provide it here to

---

### ./app/cmd/app/webview.go

**Type**: TODO

**TODO**: (jmorganca): this isn't working yet since it needs to be set

---

### ./app/logrotate/logrotate.go

**Type**: TODO

**TODO**: (jmorgan): this most likely doesn't need it's own

---

### ./app/updater/updater_darwin_test.go

**Type**: TODO

**TODO**: - a failure mode where we revert the backup

---

### ./app/updater/updater_test.go

**Type**: TODO

**TODO**: - wire up the redirects to mimic real behavior

---

### ./app/updater/updater_test.go

**Type**: TODO

**TODO**: - wire up the redirects to mimic real behavior

---

### ./app/updater/updater_windows.go

**Type**: TODO

**TODO**: should we linger for a moment and check to make sure it's actually running by checking the pid?

---

### ./app/updater/updater_darwin.go

**Type**: TODO

**TODO**: use UpgradeLogFile to record the upgrade details from->to version, etc.

---

### ./app/updater/updater_darwin.go

**Type**: TODO

**TODO**: actually inspect the error and look for permission problems before trying chown

---

### ./app/wintray/tray.go

**Type**: TODO

**TODO**: clean up exit handling

---

### ./app/wintray/eventloop.go

**Type**: TODO

**TODO**: - does this need adjusting?

---

### ./app/wintray/eventloop.go

**Type**: TODO

**TODO**: - does this need adjusting?

---

### ./app/wintray/eventloop.go:		case 0x405

**Type**: TODO

**TODO**: - how is this magic value derived for the notification left click

---

### ./app/wintray/eventloop.go

**Type**: TODO

**TODO**: - revamp how detecting an update is notified to the user

---

### ./app/store/store.go

**Type**: TODO

**TODO**: (parthsareen): temporary for experimentation

---

### ./integration/model_arch_test.go

**Type**: TODO

**TODO**: - fiddle with context size

---

### ./integration/api_test.go

**Type**: TODO

**TODO**: - is the lack of done reason on non-stream a bug?

---

### ./runner/ollamarunner/runner.go

**Type**: TODO

**TODO**: (jessegross): Ingest cached history for grammar

---

### ./runner/ollamarunner/runner.go

**Type**: TODO

**TODO**: (jmorganca): make this n_batch

---

### ./runner/ollamarunner/runner.go

**Type**: TODO

**TODO**: (jmorganca): we should send this back

---

### ./runner/ollamarunner/runner.go

**Type**: TODO

**TODO**: (jessegross): LoRA loading

---

### ./runner/ollamarunner/runner.go

**Type**: TODO

**TODO**: : support embeddings

---

### ./runner/llamarunner/runner.go

**Type**: TODO

**TODO**: (jmorganca): make this n_batch

---

### ./runner/llamarunner/runner.go

**Type**: TODO

**TODO**: (jmorganca): processBatch should be simplified, removing:

---

### ./runner/llamarunner/runner.go

**Type**: TODO

**TODO**: (jmorganca): we should send this back

---

### ./server/manifest.go

**Type**: TODO

**TODO**: (mxyng): use something less brittle

---

### ./server/sched_test.go

**Type**: TODO

**TODO**: - add one scenario that triggers the bogus finished event with positive ref count

---

### ./server/internal/cache/blob/cache.go

**Type**: TODO

**TODO**: (bmizerany): support shards

---

### ./server/internal/cache/blob/cache.go

**Type**: TODO

**TODO**: (bmizerany): test this happens only if the blob was found to

---

### ./server/internal/cache/blob/cache.go

**Type**: TODO

**TODO**: (bmizerany): reuse empty dirnames if exist

---

### ./server/internal/cache/blob/cache.go

**Type**: TODO

**TODO**: : Do the hash check, but give caller a way to skip it.

---

### ./server/internal/cache/blob/casecheck_test.go

**Type**: TODO

**TODO**: (bmizerany): Print platform-specific instructions or

---

### ./server/internal/manifest/manifest.go

**Type**: TODO

**TODO**: : Define more specifically how to represent data types as strings.

---

### ./server/internal/internal/names/name_test.go

**Type**: TODO

**TODO**: : {"n:t/m:t", Name{}},

---

### ./server/internal/internal/names/name_test.go

**Type**: TODO

**TODO**: : {"/h/n/m:t", Name{}},

---

### ./server/internal/registry/server.go

**Type**: TODO

**TODO**: (bmizerany): Write a test to ensure that we are logging

---

### ./server/internal/client/ollama/registry.go

**Type**: TODO

**TODO**: (bmizerany): add a "commit" trace event

---

### ./server/internal/client/ollama/registry.go

**Type**: TODO

**TODO**: (bmizerany): work to remove the need to do this

---

### ./server/internal/client/ollama/registry.go

**Type**: TODO

**TODO**: (bmizerany): return digest here

---

### ./server/internal/client/ollama/registry.go

**Type**: TODO

**TODO**: (bmizerany): clone client.Transport, set

---

### ./server/internal/client/ollama/registry_synctest_test.go

**Type**: TODO

**TODO**: : go:build goexperiment.synctest

---

### ./server/quantization.go

**Type**: TODO

**TODO**: 

---

### ./server/routes.go

**Type**: TODO

**TODO**: (jmorganca): avoid building the response twice both here and below

---

### ./server/routes.go

**Type**: TODO

**TODO**: @nicolepardal: avoid reaching into kvData here; pass required tokenizer metadata via model/options instead

---

### ./server/routes.go

**Type**: TODO

**TODO**: : this first normalization should be done by the model

---

### ./server/routes.go

**Type**: TODO

**TODO**: (bmizerany): Decide if we want to make this

---

### ./server/routes.go

**Type**: TODO

**TODO**: (drifkin): this is from before we added proper thinking support.

---

### ./server/prompt.go

**Type**: TODO

**TODO**: : Ideally we would compute this from the projector metadata but some pieces are implementation dependent

---

### ./kvcache/causal.go

**Type**: TODO

**TODO**: (jessegross): We should check to see if removing the middle of the sequence will

---

### ./parser/parser.go

**Type**: TODO

**TODO**: : single quotes

---

### ./thinking/template.go

**Type**: TODO

**TODO**: (drifkin): to be more robust, check that it's in the action

---

### ./ml/backend/ggml/ggml.go

**Type**: TODO

**TODO**: : assign vision tensors to the gpu if possible

---

### ./model/vocabulary_test.go

**Type**: TODO

**TODO**: (mxyng): this is to match previous behaviour

---

### ./model/vocabulary_test.go

**Type**: TODO

**TODO**: (mxyng): this is to match previous behaviour

---

### ./model/renderers/qwen3coder_test.go

**Type**: TODO

**TODO**: (drifkin): add multiple params back once we have predictable

---

### ./model/renderers/qwen3vl.go

**Type**: TODO

**TODO**: : (jmorganca): how to render this is different for different

---

### ./model/renderers/qwen3vl.go

**Type**: TODO

**TODO**: : support videos

---

### ./model/renderers/qwen3coder.go

**Type**: TODO

**TODO**: (drifkin): it would be nice to format the JSON here similarly to

---

### ./model/renderers/qwen3coder.go

**Type**: TODO

**TODO**: (drifkin): it would be nice to format the JSON here similarly to

---

### ./model/parsers/qwen3vl.go

**Type**: TODO

**TODO**: : call the init function

---

### ./model/parsers/qwen3vl.go

**Type**: TODO

**TODO**: (drifkin): if the same turn contains multiple interleaved content

---

### ./model/parsers/qwen3coder.go

**Type**: TODO

**TODO**: (drifkin): if the same turn contains multiple interleaved content

---

### ./model/parsers/qwen3coder.go

**Type**: TODO

**TODO**: (drifkin): move this to a shared location

---

### ./model/wordpiece.go

**Type**: TODO

**TODO**: : use [UNK] from config

---

### ./model/models/mllama/model.go

**Type**: TODO

**TODO**: : attention mask, cross attention mask

---

### ./model/models/gemma3/model.go

**Type**: TODO

**TODO**: : inputProjection must be transposed since they're incompatible with visionOutputs

---

### ./model/models/gemma3/model_text.go

**Type**: TODO

**TODO**: (jmorganca): this should ideally be set to 0.0 in the

---

### ./model/models/qwen3vl/imageprocessor.go

**Type**: FIXME

**TODO**: (mxyng): the model defined longest edge (16M) is too large for the default

---

### ./model/models/deepseek2/model.go

**Type**: TODO

**TODO**: : these models haven't been vetted so skip for now

---

### ./model/models/llama/model.go

**Type**: TODO

**TODO**: : support mixtures of experts

---

### ./model/bytepairencoding.go

**Type**: TODO

**TODO**: : process special tokens concurrently

---

### ./model/bytepairencoding.go

**Type**: TODO

**TODO**: : process splits concurrently

---

### ./model/bytepairencoding.go

**Type**: TODO

**TODO**: : handle the edge case where the rune isn't in the vocabulary

---

### ./sample/samplers.go

**Type**: TODO

**TODO**: (parthsareen): update sampler interface to use json unmarshal https://github.com/ollama/ollama/issues/9278

---

### ./api/types.go

**Type**: TODO

**TODO**: : only string slices are supported right now

---

### ./openai/openai.go

**Type**: TODO

**TODO**: (https://github.com/ollama/ollama/issues/5259): support []string, []int and [][]int

---

### ./llama/llama.go

**Type**: TODO

**TODO**: : Support non-default params

---

### ./llama/llama.go

**Type**: TODO

**TODO**: : this is a temporary wrapper to allow calling C++ code from CGo

---

### ./convert/convert_gemma3n.go

**Type**: TODO

**TODO**: : handle audio and vision towers

---

### ./convert/convert_qwen3.go

**Type**: TODO

**TODO**: : handle split experts

---

