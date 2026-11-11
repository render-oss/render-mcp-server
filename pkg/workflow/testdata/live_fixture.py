"""Disposable Workflows fixture for opt-in MCP integration tests."""
import time
from render import Retry, TaskContext, Workflows

app = Workflows(default_timeout=120, default_plan="flex", default_retry=Retry(max_retries=0, wait_duration_ms=1000))

@app.task
def total(ctx: TaskContext, values: list[int]) -> dict:
    return {"rows": len(values), "total": sum(values), "largeInteger": 9007199254740993}

@app.task(retry=Retry(max_retries=2, wait_duration_ms=1000))
def failure(ctx: TaskContext) -> None:
    raise ValueError("MCP_EXPECTED_FAILURE")

@app.task
def large_result(ctx: TaskContext) -> dict:
    return {"text": "水🍃" * 12000, "largeInteger": 9007199254740993, "nested": [[1, 2], []]}

@app.task
def delayed(ctx: TaskContext, seconds: int) -> str:
    time.sleep(min(max(seconds, 0), 90))
    return "MCP_DELAYED_COMPLETE"

@app.task
async def parent(ctx: TaskContext) -> dict:
    return await ctx.run(total, [2, 5, 8])

app.start()
