from andb_sdk import AndbClient


def test_client_init() -> None:
    client = AndbClient()
    assert client.base_url.startswith("http")
