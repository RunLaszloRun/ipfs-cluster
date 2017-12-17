# Introduction

This document is a rough draft of an RFC for supporting sharded dags in ipfs-cluster.  After these ideas become more concrete, a little more thoroughly researched and have had time to incorporate changes suggested by ipfs-cluster contributors the goal is to include this RFC in ipfs/notes to get comments from the rest of the ipfs community.

This document outlines motivations for sharding dags across the nodes of an ipfs-cluster, and provides suggestions on development paths to achieve the motivated goals.  It begins by making the strongest assumptions about the data, starting with familiar unixfs dags describing ipfs files that can be stored on any single node's repo, and working up to a discussion of arbitrary ipld dags that are too large to fit on any node's repo.

# Motivation

There are two primary motivations for adding data sharding to ipfs-cluster.  See the WIP use cases documents in PR #215, issue #212, and posts on the discuss.ipfs forum (ex: https://discuss.ipfs.io/t/ubuntu-archive-on-top-of-ipfs/1579 and https://discuss.ipfs.io/t/segmentation-file-in-ipfs-cluster/1465/2 and https://discuss.ipfs.io/t/when-file-upload-to-node-that-can-ipfs-segmentation-file-to-many-nodes/1451) for some more context.  (TODO -- investigate b5/data-together use cases and document whether this shows up there, if so add to use case doc + link here)

## Motivation 1: store files too big for a single node

This is one of the most requested features for ipfs-cluster.  ipfs-pack (https://github.com/ipfs/ipfs-pack) exists to fill a similar need by allowing data stored in a POSIX filesystem to be referenced from an ipfs-node's datastore without acutally copying all of the data into the node's repo.  However certain use cases require functionality beyond ipfs-pack. For example, what if a cluster of nodes needs to download a large file bigger than any of its nodes' individual machines' disk size?  In this case using ipfs-pack is not enough, such massive files would not fit on a single node's storage device and coordination between nodes to store the file becomes essential.

Storing large files is a feature that would serve many use cases, the more general ability to store large ipld dags of arbitrary structure has the potential to support others.  As an example consider importing a cryptocurrency ledger to an ipfs cluster when no one node could hold the entire dag.

## Motivation 2: store files with different replication strategies for fault tolerance

This feature is also frequently requested, including in the original thread leading to the creation of ipfs-cluster (See issue #1), and in @raptortech-js 's thorough discussion in issue #9.  The idea is to use sharding to incorporate space-efficient replication techniques to provide better fault tolerance of storage.  This would open up ipfs-cluster to new usage patterns, especially storing archives with infrequent updates and long storage periods.

Again, while files storage will benefit from efficient, fault tolerant encodings, these properties are potentially also quite useful for storing arbitrary merkle dags. 

# Basic sharding overview

To add a file ipfs must "import" it.  ipfs chunks the file's raw bytes and builds a merkle dag out of these chunks to organize the data for storage in the ipfs repo. The format of the merkle dag nodes, the way the chunks are produced and the layout of the dag depend on configurable strategies. Usually ipfs represents the data added as a tree which mimics a unix-style hierarchy so that the data can be directly translated into a unix-like filesystem representation.

Regardless of the chunking or layout strategy, importing a file can be viewed abstractly as a process that takes in a stream of data and outputs a stream of dag nodes, or more precisely "blocks", which is the term for the representation of dag nodes actually used for storage.  As blocks represent dag nodes they contain links to other blocks.  Together these blocks and their links determine the structure of the dag built by the importing process.  Furthermore these blocks are content-addressed by ipfs, which resolves blocks by these addresses upon user request.  Although the current imorting process adds all blocks corresponding to a file to a single ipfs node, this is not important for preserving the dag structure.  ipfs nodes advertise the address of every block added to their storage repo.  If a dag's blocks exist across multiple ipfs peers the individual blocks can readily be discovered by any peer and the information the blocks carry can be put back together.  This location flexibility makes partitioning a file's data at the block level an attractive mechanism for implementing sharding in ipfs-cluster.

Therefore we propose that sharding in ipfs-cluster amounts to allocating a stream of blocks among different ipfs nodes.  Cluster should aim to do so:
1. Efficiently: best utilizing the resources available in a cluster as provided by the underlying ipfs nodes
2. Lightly: the cluster state should not carry more information than relevant to cluster, e.g. no more than is relevant for coordinating allocation of collections of blocks
3. Cleverly: the allocation of blocks might benefit from the information provided by the dag layout or ipld-format of the dag nodes.  For example cluster should eventually support grouping together logical subgraphs of the dag.

On a first approach we aim for a simple layout and format-agnostic strategy which simply groups blocks output from the importing process into a size-based unit, from here on a "shard", that fits within a single ipfs node's storage repo.

## Implementation
### Overview
In brief the approach is to pin two dags in ipfs-cluster for every sharded file.  The first is the ipfs unixfs dag created upon adding the file encoded in the blocks, and their links, output by the importing process.  Additionally ipfs-cluster will build a "cluster-dag" specifically built to track and pin the blocks of the file dag in a way that groups data into shards.  Each shard is pinned by (at least) one cluster peer.  The graph of shards is organized in a 3 level tree.  The first level is the cluster-dag root, the second represents the shards into which data is divided with at least one shard per cluster peer.  The third level lists all the cids of blocks of the file dag included in a shard:
```
The root would be like:
{
  "shards" : [
     {"/": <shard1>},
     {"/": <shard2>},
     ...
   ]
}

where each shard looks like:

{
  "blocks" : [
     {"/": <block1>},
     {"/": <block2>},
     ...
   ]
}

```
Each cluster node recursively pins a shard-node of the cluster-dag, ensuring that the blocks of file data referenced underneath the shard are pinned by that node.  The index of shards, i.e. the root, can be (non-recursively) pinned on any/all cluster nodes.

With this implementation the cluster can provide the entire original ipfs file dag on request.  For example if an ipfs peer queries for the entire file they first resolve the root node of the dag which must be pinned on some shard.  If the root has child links then ipfs peers in the cluster are guaranteed to resolve them with content, as all blocks are pinned somewhere in the cluster as children of a shard-node in the cluster-dag.  Note that this implementation conforms well to the goal of coordinating "lightly" above because the cluster state need only keep track of 1 cid per shard.  This prevents the state size from growing too large or more complex than necessary for the purpose of allocation.

The state format would change slightly to account for linking together the root cid of an ipfs file dag and the cluster-dag pinning its leaves

```
"<cid>": {
  "name": <pinname>,
  "cid": <cid>,
  "clusterdag": <cidcluster>,
  "allocations" : []
}
"<shard1-cid>": {
  "name": "<pinname>-shard1",
  "cid": <shard1-cid>,
  "clusterdag": nil,
  "allocations": [cluster peers]
}
....
```

Under this implementation replication of shards proceeds like replication of an ordinary pinned cid.  If a shard is replicated across the cluster then data is not always lost during a node failure.  To get better guarantees for less extra space we will need to proceed in the direction of the fault tolerance described in the Future Work section.


### Adding files
Currently the proposed implementation of basic sharding relies on ipfs-cluster having an incoming stream of data that can be read lazily as cluster sends off blocks to remote peers.  This allows sharding files that are too big to fit in a single repo or whose dags are too big to read into memory all at once, as resources can be cleared while streaming occurs.  Only the cids of blocks in the shard need to be maintained to construct the shard node of the cluster-dag, not the entire block or in-memory node structure.  Therefore in our basic sharding implementation we assume that sharded data must be added to ipfs-cluster directly.  This is because, as far as we know, there is not currently a way to access such a stream of blocks belonging to an already-imported-dag without potentially overloading the local ipfs daemon's resources.

The cluster node adding the file will read in a stream of bytes that are then passed to an importer to create an ipfs files dag chunked (size-splitter or rabin-splitter) and layed out (trickle or balanced) per user instruction.  Before importing blocks of each shard the cluster peer calls the `allocate()` function to find the pinning peer for the upcoming shard.  Concurrent with ipfs files dag creation and transmission of the dag's blocks to the allocated cluster peer, the importing ipfs-cluster node will oversee construction of a cluster-dag shard node using the cids of the blocks of the ipfs files dag.  Once a shard is filled with cid references it will also be transmitted and pinned to the allocated ipfs-cluster node.  This process continues until both dags are fully constructed and pinned as expected.  The importing node transmits blocks by calling `ipfs block put` and `ipfs pin` on the pinning node's ipfs daemon via an RPC to the pinning cluster peer.   The pinning node should check the state to ensure it was actually assigned the pin before running the rpc to completion.  Note that this will be one of the more involved parts of implementing basic sharding and will involve extracting, refactoring and adding features to the dag building functionality of go-ipfs.

There will likely be several endpoints entering into the functionality for adding sharded data.  For example, there will be a go endpoint `Add(r io.Reader, opts...)` which just adds arbitrary data. There will be an HTTP endpoint to which users can post multipart data just like it is now posted to ipfs and which creates the unixfs merkle dag.  If dags can be streamed node by node through ipfs safely without possibly overloading the process and its repo, cluster will likely support an AddDag(chan <-*dagNode) endpoint which allocates already-imported-dags.

We will likely abstract the importing functionality into an importer component of cluster.  Following current conventions other cluster modules would communicate with this component over RPC.  However, the RPC module we use does not handle transmitting streams of data so this component and the components containing the endpoints described above will need to expose a stream friendly abstraction, potentially an HTTP endpoint or socket.

As part of polishing this process we will eventually need to polish cluster's UX around sharding.  We will likely want `ipfs-cluster-ctl` commands for adding files, ex `ipfs-cluster-ctl add` beyond the current ipfs proxy endpoint method of adding to cluster and will probably want something like a `--shard` flag for this command.  Additionally "How much room do I have in my cluster?" is a very predictable question that users importing huge files will want to know.  We should add cluster cli tools (something like ipfs metrics, or the still-unmade connectivity-graph) that inform users how much space they have available throughout the entire cluster, and the largest recommended file the cluster can ingest.  Perhaps later on we can get even fancier and include predictions of insertion time, and a visualization of how many shards of what size will be added.


# Future work

## Fault tolerance
Disclaimer: this needs more work and that work will go into its own RFC. This section provides a basis upon which we can build.  It is included to demonstrate that the current sharding model works well for implementing this important extension.  We will bring this effort back into focus once the prerequisite basic sharding discussed above is implemented.

### Background reading
This [report on RS coding for fault tolerance in RAID-like Systems by Plank](https://web.eecs.utk.edu/~plank/plank/papers/CS-96-332.pdf) is a very straightforward and practical guide.  It has been helpful in planning out how to approach fault tolerance in cluster and will be very helpful when we actually implement.  It has an excellent description of how to implement the algorithm that calculates the code from data, and how to implement recovery.  Furthermore one of his example use case studies include algorithms for initialization of the checksum values that will be useful when replicating very large files.

### Proposed implementation
#### Overview
The general idea of fault tolerant storage is to store your data in such a way that if several machines go down all of your data can be reconstructed.  Of course you could simply store all of your data on every machine in your cluster, but many clever approaches use data sharding and techniques like erasure codes to achieve the same result with fewer bits stored.  A standard approach is to store shards of data across multiple data devices and then store some kind of checksum information in separate, checksum devices.  It is a simple matter to extend the basic sharding implementation above to work well in this paradigm.  When storing a file in a fault tolerant configuration ipfs-cluster, as in basic sharding, will store the ipfs files dag without its leaves and an cluster-dag.  However now the cluster-dag has additional shards not referencing the leaves of the ipfs files dag, but rather to checksum data taken over all the file's data.  For an m out of n encoding:

```
The root would be like:
{
  "parts" : [
     {"/": <part1>},
     {"/": <part2>},
     ...
     {"/": <partN>},
   ]
   "checksums" : [
     {"/": <chksum1>},
     {"/": <chksum2>},
     ...
     {"/": <chksumM>},     
}
```

While there are several RAID modes using different configurations of erasure codes and data to checksum device ratios, my opinion is that we probably can ignore most of these as using m,n RS coding is superior in terms of space efficiency for fault tolerance gained.  However different RAID modes have different time efficiency properties in their original setting anyway.  It is unclear if implementing something (maybe) more time efficient but less space efficient and fault tolerant than RS has much value in ipfs-cluster.  I lean towards no but I should investigate further. (TODO -- answer these questions, any counter example use cases?  any big gains for using other RAID modes that help these use cases?) On another note in Feb 2019 the tornado code patent is set to expire (\o/) and we could check back in then and look into the feasability of using (perhaps implementing if no OSS exists yet?!?) tornado codes (which are faster).  There are others we'll want to check the legal/implementation situation for (biff codes) so pluggability is important.

Overall this is pretty cool for users because the original dag (recall how basic sharding works) and the original data exist within the cluster.  This way users can query any cid from the original dag and the cluster ipfs nodes will seamlessly provide it, all while silently and with very efficient overhead they are protecting the data from a potentially large number of peer faults.

We have some options for allowing users to specify this mode.  It could be a cluster specific flag to the "add" endpoint or a config option setting a cluster wide default.

#### Importing with checksums
If memory/repo size constraints are not a limiting factor it should be straightforward for the cluster-dag importer running on the adding node to keep a running tally of the checksum values and then allocate them to nodes after getting every data shard pinned.  Note this claim is specific to RS as the coding calculations are simple linear combinations of operations and everything commutes, while I wouldn't be surprised if potential future codes also had this property it is something we'll need to check up on once we get serious about pluggability.

If we are in a situation where shards approach the size of ipfs repo or working memory then we can gather inspiration from the report by Plank, specifically the section "Implementation and Performance Details: Checkpointing Systems".  In this section Plank outlines two algorithms for setting checksum values after the data shards are already stored by sending messages over the network.  From my first read-through the broadcast algorithm looks the most promising.  This algorithm would allow cluster to send shards one at a time to the peer holding a zeroed out checksum shard and then perform successive updates to calculate the checksums, rather than requiring that the cluster-dag importer hold the one shard being filled up for pinning alongside the m checksum shards being calculated.

## Clever sharding
Future work on sharding should aim to keep parts of the dag that are typically fetched together within the same shard.  This feature should be able to take advantage of particular chunking and layout algorithms, for example grouping together subdags representing packages when importing a file as a linux package archive.  It would also be nice to have some techniqes, possibly pluggable, available for intelligently grouping blocks of an arbitrary dag based on the dag structure.

## Sharding existing dags
This would require new features in go-ipfs if ipfs-cluster is to stream dag nodes from its local daemon.  Specifically the ipfs api would somehow need to include an endpont that provides a stream of a dag's blocks without overcommitting the daemon's resources (i.e. downloading the whole dag first if the repo is too small).  An effort that will bring sharding existing dags closer to reality is the [CAR (Certified ARchives) project](https://github.com/ipld/specs/pull/51) which aims to define a format for storing dags.  One of its goals as stated in a recent proposal is to explicitly allow users to "Traverse a large dag streamed over HTTP without downloading the entire thing."  Cluster could certainly shard dag nodes extracted from a streamed CAR file, either kept on disk or provided at a location on the network.  CAR integration with go-ipfs could potentially allow resource aware streaming of dag nodes over the ipfs daemon's api so that the sharding cluster peer need only know the dag's root hash.

# Workplan

There is a fair amount that needs to be built, updated and serviced to make all of this work.  Here I am focusing on basic sharding and allowing support for files that wouldn't fit in an ipfs repo.  Fault tolerance, clever sharding and support for sharding existing dags will come after this is nailed down.  

* Build up a version of the `importers` (aka `dagger` aka `dex`) project that suits cluster's needs (if possible keep it general enough for general use).  In short we want a library that imports data to ipfs dags with arbitrary chunking and layout algorithms.  For our use case it is important that the interfaces support taking in a stream of data and returning a stream of blocks or dag nodes.
* Figure out how unixFS file/directory data can be streamed (via reading from disk directly or via http POST multipart upload)
* Implement a cluster importer component.  It will need to provide endpoints that supporting streams as input and output.
* Create a cluster add endpoint that uses the cluster importer component to split data and create dags.  Put the output blocks into the local ipfs daemon and pin each block with cluster.  This will require figuring out the importer component's api, including the REST API part.
* Include, in the add endpoint, creation of the cluster-dag and pin blocks by way of pinning shards.  This is still all confined to one cluster peer.  This stage will require defining a cluster ipld-format (if we go that route)
* Update the state format so that pinned cids reference clusterdags and so that the state differentiates between cids and shards.  Before doing this the state migration code should be generalized so that only a single migration function is needed (Issue #230)
* Build support for allocation of different cluster-dags to different peers.  This includes implementing RPC transmission of all chunks of a given shard to the allocated peer.
* Work to make the state format scale to large numbers of cids
* Add in usability features: don't make sharding default on add endpoint but trigger with --shard, maybe make configs that can set sharding as default or importer defaults, allow add to specify different chunking and importing algorithms like ipfs, ipfs add proxy endpoint can also take in --shard and then calls same functionality as add endpoint, add in tools useful for managing big downloads like reporting the total storage capacity of cluster or the expected download time.
* Testing should happen throughout and we should have a plan with regards to which tests we run in place early (maybe before we start).  Eventually we will want a huge cluster in the cloud with a few TB of storage for ingesting huge files
* Move on beyond basic sharding and start design process again to support clever techniques for certain dags/usecases, sharding existing dags, and fault tolerance