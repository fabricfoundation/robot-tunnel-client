"""Parse Fabric tunnel Action Event payloads."""

import json
from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class ActionEvent:
    action: str
    params: dict[str, Any] = field(default_factory=dict)
    timestamp: str = ""


def parse_action_event(raw: bytes) -> Optional[ActionEvent]:
    """Parse a Fabric Action Event from raw bytes.

    Expected schema (tunnel handlers.go:97-104)::

        {
          "payload": {"action": "move_forward", "params": {"speed": 0.5}},
          "transaction_details": {...},
          "timestamp": "2026-01-01T00:00:00Z"
        }

    Returns None on parse failure.
    """
    try:
        event = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return None

    if not isinstance(event, dict):
        return None

    payload = event.get("payload") or {}
    if not isinstance(payload, dict):
        return None

    return ActionEvent(
        action=payload.get("action", "stop"),
        params=payload.get("params") or {},
        timestamp=event.get("timestamp", ""),
    )
