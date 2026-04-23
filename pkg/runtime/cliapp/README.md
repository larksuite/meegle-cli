# `pkg/runtime/cliapp`

Assembles the framework core into terminal-facing CLI products.

Current responsibilities:

- build `router -> pipeline -> backend -> output`
- expose terminal execution via `Execute` / `ExecuteWithIO`
- expose programmatic CLI-path execution via `Invoke`
- keep `cobra` as the only parser/router implementation
