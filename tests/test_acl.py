import pytest
from clawbridge.auth.acl import PolicyEngine


def test_exact_channel_and_action_match():
    rules = ["slack:post_message", "jira:read_issue"]
    assert PolicyEngine.is_authorized(rules, channel="slack", action="post_message") is True
    assert PolicyEngine.is_authorized(rules, channel="jira", action="read_issue") is True
    assert PolicyEngine.is_authorized(rules, channel="jira", action="delete_issue") is False
    assert PolicyEngine.is_authorized(rules, channel="twitter", action="post_tweet") is False


def test_channel_wildcard():
    rules = ["slack:*", "discord:send_message"]
    assert PolicyEngine.is_authorized(rules, channel="slack", action="post_message") is True
    assert PolicyEngine.is_authorized(rules, channel="slack", action="add_reaction") is True
    assert PolicyEngine.is_authorized(rules, channel="slack", action="delete_channel") is True
    assert PolicyEngine.is_authorized(rules, channel="discord", action="send_message") is True
    assert PolicyEngine.is_authorized(rules, channel="discord", action="kick_member") is False


def test_action_prefix_wildcard():
    rules = ["*:read*", "slack:post_message"]
    assert PolicyEngine.is_authorized(rules, channel="jira", action="read_issue") is True
    assert PolicyEngine.is_authorized(rules, channel="github", action="read_repo") is True
    assert PolicyEngine.is_authorized(rules, channel="jira", action="create_issue") is False


def test_global_wildcard_grants_all():
    rules = ["*"]
    assert PolicyEngine.is_authorized(rules, channel="slack", action="post_message") is True
    assert PolicyEngine.is_authorized(rules, channel="jira", action="delete_project") is True
    assert PolicyEngine.is_authorized(rules, channel="twitter", action="any_op") is True


def test_empty_rules_denies_all():
    assert PolicyEngine.is_authorized([], channel="slack", action="post_message") is False

