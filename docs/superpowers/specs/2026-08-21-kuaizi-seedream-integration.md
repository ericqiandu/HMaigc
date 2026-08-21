# Kuaizi Seedream 5.0 Integration Design

## Goal

Add `seedream5.0lite` and `seedream5.0pro` as production-grade image models managed by the existing Kuaizi provider account, without adding a parallel UI model catalog or reusing the incompatible GPT Image 2 protocol.

## Scope

- Register a first-party `kuaizi/seedream` provider family with two image models.
- Publish dynamic image capabilities to the existing model catalog.
- Support text-to-image and one-to-many-reference image editing.
- Support 2K and 3K outputs for the ratios published by the registry.
- Create and poll the documented asynchronous Seedream endpoints.
- Preserve task identity, upstream trace identity, failure reason, and generated image assets.
- Keep billing, task ownership, watermark policy, and media hydration on existing shared infrastructure.

The current canvas batch layer already creates one backend task per requested image. Every Seedream upstream task therefore generates exactly one image. `seedream5.0lite` sends `sequential_image_generation=disabled`; `seedream5.0pro` omits that unsupported field. This prevents double fan-out, duplicate charging, and ambiguous node ownership.

## Provider Contract

### Models

| Model | Reference images | Output per upstream task | Watermark | Resolutions |
| --- | ---: | ---: | --- | --- |
| `seedream5.0lite` | 0-14 | 1 | controlled | 2K, 3K |
| `seedream5.0pro` | 0-10 | 1 | controlled | 2K, 3K |

Both models publish the ratios `1:1`, `16:9`, `9:16`, `4:3`, `3:4`, `3:2`, `2:3`, `21:9`, and `9:21`. The UI continues to consume these values from the backend catalog and must not contain a model-name allowlist.

### Image sizing

The existing GPT Image 2 contract interprets a resolution label as a longest-edge target. Seedream instead validates total output pixels. The shared capability DTO therefore gains `resolutionPixels: Record<string, number>`:

- `2K`: 4,194,304 target pixels
- `3K`: 9,437,184 target pixels

The web dimension builder derives width and height from the selected aspect ratio and target pixel budget, aligns both dimensions to 16 pixels, and verifies the documented Seedream range of 3,686,400-10,404,496 pixels. Models without `resolutionPixels` retain the existing longest-edge behavior.

### Create request

`POST /ai-open-platform-api/v1/seedream/image/task/create`

The adapter sends:

- `prompt`
- `model`
- `size` as validated `WIDTHxHEIGHT`
- `image` when reference images are present
- the frozen account watermark parameter when applicable
- `output_format: "jpeg"`
- `sequential_image_generation: "disabled"` only for Lite

The adapter explicitly rejects blank prompts, unknown models, masks, video/audio references, non-public reference URLs, counts other than one, reference-count overflow, invalid dimensions, and unsupported size labels. There is no model fallback.

### Poll request

`POST /ai-open-platform-api/v1/seedream/image/task/status`

Polling runs no faster than once every five seconds. Only `running`, `succeeded`, and `failed` are accepted. A successful single-image task must return exactly one valid HTTPS URL. The image is downloaded immediately and returned through the existing backend result contract so the normal resource upload path persists it.

The adapter rejects mismatched task IDs, unknown states, malformed JSON, non-zero business codes, invalid image URLs, non-image downloads, and responses without the documented fields.

## Failure and Observability Contract

Every upstream response is decoded from the gateway wrapper (`HTTP 200` plus `code == 0`). Errors include the upstream `trace_id` when present. Failed task errors include `task_id`, upstream `error`, and `trace_id`. Retryable poll transport failures retain the existing bounded polling behavior; create ambiguity uses `KuaiziCompatibleCreateError` so task resume semantics remain intact.

No error is hidden by a default model, alternate endpoint, fixed-size fallback, or synthetic result.

## Dispatch and Publication

The Kuaizi compatible worker dispatches image tasks by registered family:

- `gpt-image2` -> existing GPT Image 2 adapter
- `seedream` -> new Seedream adapter

Dispatching by capability alone is forbidden because the two image families have incompatible endpoints and payloads.

The existing provider publication transaction publishes the new family into the managed Kuaizi channel, binds the healthy frozen credential, creates disabled/unpriced model facts, and records an administrator audit event. Models remain unavailable to users until pricing is explicitly configured.

## Verification

- Registry tests cover both model identities, dynamic capabilities, maximum reference counts, watermark control, and sizing metadata.
- Web tests cover pixel-budget dimension derivation while preserving GPT Image 2 longest-edge behavior.
- Adapter tests cover Lite and Pro payload differences, reference limits, dimensions, successful polling, business failures with trace IDs, mismatched task IDs, and multi-result rejection.
- Worker tests cover family-specific dispatch and rejection of unimplemented families.
- Final gates: focused Go tests, full backend tests, affected web tests, TypeScript build, and repository diff review.

Live upstream generation is a separate cost-bearing acceptance gate and is not required for source completion. When executed, it must use a published, priced model and record the real task ID, trace ID, billing order, and persisted resource.
