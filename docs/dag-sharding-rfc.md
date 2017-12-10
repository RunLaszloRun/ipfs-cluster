# Introduction
This document is a rough draft of an RFC for supporting sharded dags in ipfs-cluster.  After these ideas become more concrete, a little more thoroughly researched and have had time to incorporate changes suggested by ipfs-cluster contributors the goal is to include this RFC in ipfs/notes to get comments from the rest of the ipfs community.

This document outlines motivations for sharding dags across the nodes of an ipfs-cluster, and provides suggestions on development paths to achieve the motivated goals.  The "Thoughts on implementation" section describes development of sharding features.  It begins by making the strongest assumptions about the data, starting with familiar unixfs dags describing ipfs files that can be stored on any single node's repo, and working up to a discussion of arbitrary ipld dags that are too large to fit on any node's repo.

# Motivation
There are two primary motivations for adding data sharding to ipfs-cluster.  See the WIP use cases documents in PR #215, issue #212, and posts on the discuss.ipfs forum (ex: https://discuss.ipfs.io/t/ubuntu-archive-on-top-of-ipfs/1579 and https://discuss.ipfs.io/t/segmentation-file-in-ipfs-cluster/1465/2 and https://discuss.ipfs.io/t/when-file-upload-to-node-that-can-ipfs-segmentation-file-to-many-nodes/1451) for some more context.  (TODO -- investigate b5/data-together use cases and document whether this shows up there, if so add to use case doc + link here)

## Use case 1: store files too big for a single node
This is one of the most requested features for ipfs-cluster.  ipfs-pack (https://github.com/ipfs/ipfs-pack) exists to fill a similar need, as far as I understand it ipfs-pack allows data stored in a POSIX filesystem to be referenced from an ipfs-node's datastore without acutally copying all of the data into the nodes repo.  However certain use cases require functionality out of scope of ipfs-pack. For example, what if a cluster needs to download a large file bigger than any of its nodes individual machines' disk size?  Supporting functionality for adding this file to ipfs across cluster nodes in ipfs-cluster would provide a simple path for adding large files to ipfs.  Even if the large file is stored locally on a storage device accessible to a cluster node's machine, and therefore could be referenced by ipfs-pack, users may want to keep data directly in ipfs repos.  (TODO -- evaluate this more precisely, are there specific instances where this is the case, ex, discoverability and latency issues with ipfs-pack? Would improving ipfs-pack / datastore integration better serve users in these cases?).  Additionally even using ipfs-pack, truly massive files might still not be able to fit on a single storage device on an ipfs node and in this case coordination between nodes to store the file becomes essential.

Although storing large files is a natural feature that would serve many use cases, the ability to store large ipld dags of arbitrary structure is something with plenty of conceivable use cases, for example importing a cryptocurrency ledger to an ipfs cluster, of which no one node could hold the entire dag.

## Use case 2: store files with different replication strategies for fault tolerance
This use case is brought up from time to time, including in the original thread leading to the creation of ipfs-cluster (See issue #1), and in @raptortech-js 's thorough discussion in issue #9.  As I understand it the idea is to bring RAID modes of storage (including modes that incorporate sophisticated FEC techniques) to provide better fault tolerance for a given space efficiency of storage.  This would open up ipfs-cluster to new usage patterns, especially storing archives with infrequent reads and indefinite storage periods.

Again, while files storage could benefit from efficient, fault tolerant encodings, these properties are potentially desirable for the long term storage of arbitrary merkle dags as well. 

# Thoughts on implementation

## Small dags

### Starting point pseudo-RAID replication with small unixfs files

### true RAID replication modes with small unixfs files

### true RAID replication modes with small general dags


## Large dags

### Collaborative importing

### Arbitrary dag considerations











