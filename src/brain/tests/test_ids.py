# C04: version 7 and not 4, because a random id scatters the inserts and fragments the index.

from infra.ids import new


def test_the_generated_id_is_a_uuid_v7() -> None:
    assert all(new().version == 7 for _ in range(100))


def test_two_consecutive_ids_sort_in_the_order_they_were_created() -> None:
    first = new()
    second = new()

    assert second > first
