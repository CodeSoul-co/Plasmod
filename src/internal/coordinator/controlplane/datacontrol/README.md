# Data Coordinator

Data control (`datacontrol`) is the control-plane component used to organize data nodes and segment allocation inside the imported ANDB control-plane subtree.

## Dependency

- KV store: a kv store has all the metadata datacontrol needs to operate. (etcd)
- Message stream: a message stream to communicate statistics information with data nodes. (Pulsar)
- Root Coordinator: timestamp, id and meta source.
- Data Node(s): could be an instance or a cluster, actual worker group handles data modification operations.
