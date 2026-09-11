import utils


class TestClamp:
    def test_returns_value_within_bounds(self):
        assert utils.clamp(0.5, 0.0, 1.0) == 0.5

    def test_clamps_below_lower_bound(self):
        assert utils.clamp(-1.0, 0.0, 1.0) == 0.0

    def test_clamps_above_upper_bound(self):
        assert utils.clamp(2.0, 0.0, 1.0) == 1.0

    def test_lower_bound_is_inclusive(self):
        assert utils.clamp(0.0, 0.0, 1.0) == 0.0

    def test_upper_bound_is_inclusive(self):
        assert utils.clamp(1.0, 0.0, 1.0) == 1.0

    def test_negative_bounds(self):
        assert utils.clamp(-5.0, -10.0, -2.0) == -5.0
        assert utils.clamp(-20.0, -10.0, -2.0) == -10.0
        assert utils.clamp(0.0, -10.0, -2.0) == -2.0
