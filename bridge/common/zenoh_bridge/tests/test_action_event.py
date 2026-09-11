import json

import action_event
import pytest
from action_event import ActionEvent, parse_action_event


class TestParseActionEvent:
    def test_parses_valid_event(self):
        raw = json.dumps(
            {
                "payload": {"action": "move_forward", "params": {"speed": 0.5}},
                "transaction_details": {"hash": "0xabc"},
                "timestamp": "2026-01-01T00:00:00Z",
            }
        ).encode()
        event = parse_action_event(raw)
        assert isinstance(event, ActionEvent)
        assert event.action == "move_forward"
        assert event.params == {"speed": 0.5}
        assert event.timestamp == "2026-01-01T00:00:00Z"

    def test_defaults_action_to_stop_when_payload_empty(self):
        raw = json.dumps({"payload": {}}).encode()
        event = parse_action_event(raw)
        assert event == ActionEvent(action="stop", params={}, timestamp="")

    def test_defaults_when_payload_field_missing(self):
        raw = json.dumps({"transaction_details": {}}).encode()
        event = parse_action_event(raw)
        assert event == ActionEvent(action="stop", params={}, timestamp="")

    def test_preserves_params_when_provided(self):
        raw = json.dumps({"payload": {"params": {"x": 1, "y": 2}}}).encode()
        event = parse_action_event(raw)
        assert event.params == {"x": 1, "y": 2}
        assert event.action == "stop"

    def test_timestamp_defaults_to_empty_string(self):
        raw = json.dumps({"payload": {"action": "sit"}}).encode()
        event = parse_action_event(raw)
        assert event.timestamp == ""

    def test_timestamp_is_taken_from_top_level_event(self):
        raw = json.dumps(
            {"payload": {"action": "stand"}, "timestamp": "2026-02-02T00:00:00Z"}
        ).encode()
        event = parse_action_event(raw)
        assert event.timestamp == "2026-02-02T00:00:00Z"

    @pytest.mark.parametrize("raw", [b"", b"{", b"not json", b"[]", b"null", b"42"])
    def test_malformed_or_non_object_json_returns_none(self, raw):
        assert parse_action_event(raw) is None

    def test_non_dict_payload_returns_none(self):
        raw = json.dumps({"payload": "oops"}).encode()
        assert parse_action_event(raw) is None

    def test_non_dict_params_default_to_empty_dict(self):
        raw = json.dumps({"payload": {"action": "wave", "params": []}}).encode()
        event = parse_action_event(raw)
        assert event == ActionEvent(action="wave", params={}, timestamp="")

    def test_none_payload_is_treated_as_empty(self):
        raw = json.dumps({"payload": None}).encode()
        event = parse_action_event(raw)
        assert event == ActionEvent(action="stop", params={}, timestamp="")

    def test_exports_match_module_public_names(self):
        assert action_event.ActionEvent is ActionEvent
        assert action_event.parse_action_event is parse_action_event
