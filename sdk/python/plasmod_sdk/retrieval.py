import ctypes
from ctypes import c_char_p, c_int, POINTER


class RetrievalLib:
    def __init__(self, lib_path: str):
        self.lib = ctypes.CDLL(lib_path)
        self.lib.andb_version.restype = c_char_p
        self.lib.andb_dense_search.argtypes = [c_char_p, c_int, POINTER(c_char_p), c_int]
        self.lib.andb_dense_search.restype = c_int

    def version(self) -> str:
        return self.lib.andb_version().decode("utf-8")

    def dense_search(self, query: str, top_k: int = 5) -> list[str]:
        out = (c_char_p * top_k)()
        n = self.lib.andb_dense_search(query.encode("utf-8"), top_k, out, top_k)
        return [out[i].decode("utf-8") for i in range(n)]
