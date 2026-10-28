import asyncio
import pytest
from clawbridge.vault.single_flight import SingleFlightGroup


@pytest.mark.asyncio
async def test_single_flight_deduplicates_concurrent_calls():
    group = SingleFlightGroup()
    call_count = 0
    
    async def expensive_token_refresh():
        nonlocal call_count
        call_count += 1
        await asyncio.sleep(0.05)  # simulate network roundtrip to Slack/Google OAuth
        return {"access_token": "fresh_token_xyz", "call_number": call_count}
    
    # Launch 25 concurrent callers with the identical credential key
    results = await asyncio.gather(*[
        group.execute("credential_slack_main", expensive_token_refresh)
        for _ in range(25)
    ])
    
    # Crucial assertion: the refresh function was called EXACTLY ONCE
    assert call_count == 1
    
    # All 25 callers received the exact same fresh token result
    for r in results:
        assert r["access_token"] == "fresh_token_xyz"
        assert r["call_number"] == 1


@pytest.mark.asyncio
async def test_single_flight_propagates_exception_to_all_callers():
    group = SingleFlightGroup()
    call_count = 0
    
    async def failing_token_refresh():
        nonlocal call_count
        call_count += 1
        await asyncio.sleep(0.02)
        raise RuntimeError("OAuth provider 500 server error")
        
    results = await asyncio.gather(
        *[group.execute("credential_fail_key", failing_token_refresh) for _ in range(10)],
        return_exceptions=True
    )
    
    assert call_count == 1
    assert len(results) == 10
    for r in results:
        assert isinstance(r, RuntimeError)
        assert "OAuth provider 500" in str(r)


@pytest.mark.asyncio
async def test_single_flight_cleans_up_and_allows_subsequent_calls():
    group = SingleFlightGroup()
    call_count = 0
    
    async def refresh_op():
        nonlocal call_count
        call_count += 1
        return f"result_{call_count}"
        
    first_res = await group.execute("key1", refresh_op)
    assert first_res == "result_1"
    assert call_count == 1
    
    # After first finishes, a new call executes afresh
    second_res = await group.execute("key1", refresh_op)
    assert second_res == "result_2"
    assert call_count == 2

