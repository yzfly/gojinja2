# Contributing / 参与贡献

**English** — gojinja2 has one golden rule: *CPython is the ground truth.* Any claim
about engine behavior must be backed by a fixture generated from the official
Jinja2 (pallets/jinja 3.1.6), never by a hand-written expectation.

**中文** —— gojinja2 只有一条黄金法则：*CPython 是唯一事实标准。*关于引擎行为的任何
主张都必须由官方 Jinja2（pallets/jinja 3.1.6）生成的语料支撑，禁止手写期望值。

## Workflow / 工作流

1. Fetch the reference implementation / 拉取参考实现:

   ```bash
   git clone --depth 1 --branch 3.1.6 https://github.com/pallets/jinja.git reference/jinja
   ```

2. Add cases to the corpus generators / 在语料生成器中添加用例:
   - `tools/gen_lexer_fixtures.py` — token-level / token 级
   - `tools/gen_parser_fixtures.py` — AST-level / AST 级
   - `tools/gen_render_fixtures.py` — render-level / 渲染级

3. Regenerate fixtures and run tests / 重新生成语料并运行测试:

   ```bash
   PYTHONPATH=reference/jinja/src python3 tools/gen_render_fixtures.py > rendertest/testdata/render_fixtures.json
   go test ./...
   ```

4. If your case exposes a real divergence, fix the engine — or, if the divergence
   is fundamental (e.g. int64 vs arbitrary precision), register it in
   `KNOWN_DIVERGENCES` with a justification. Silent skips are not accepted.

   如果用例暴露了真实差异：修引擎；若差异是根本性的（如 int64 与任意精度），
   在 `KNOWN_DIVERGENCES` 中登记并给出理由。不接受静默跳过。

## Code style / 代码风格

- `gofmt` + `go vet` clean.
- Comments in Chinese (project convention); exported API names follow Go conventions.
  注释使用中文（项目约定）；导出 API 命名遵循 Go 惯例。
- When porting from `reference/jinja`, cite the source function in a comment.
  从 `reference/jinja` 移植时，在注释中注明对应的源函数。
