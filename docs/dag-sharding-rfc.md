# Introduction
This document is a rough draft of an RFC for supporting sharded dags in ipfs-cluster.  After these ideas become more concrete, a little more thoroughly researched and have had time to incorporate changes suggested by ipfs-cluster contributors the goal is to include this RFC in ipfs/notes to get comments from the rest of the ipfs community.

This document outlines motivations for sharding dags across the nodes of an ipfs-cluster, and provides suggestions on development paths to achieve the motivated goals.  It begins by making the strongest assumptions about the data, starting with familiar unixfs dags describing ipfs files that can be stored on any single node's repo, and working up to a discussion of arbitrary ipld dags that are too large to fit on any node's repo.

# Motivation
There are two primary motivations for adding data sharding to ipfs-cluster.  See the WIP use cases documents in PR #215, issue #212, and posts on the discuss.ipfs forum (ex: https://discuss.ipfs.io/t/ubuntu-archive-on-top-of-ipfs/1579 and https://discuss.ipfs.io/t/segmentation-file-in-ipfs-cluster/1465/2 and https://discuss.ipfs.io/t/when-file-upload-to-node-that-can-ipfs-segmentation-file-to-many-nodes/1451) for some more context.  (TODO -- investigate b5/data-together use cases and document whether this shows up there, if so add to use case doc + link here)

## Motivation 1: store files too big for a single node
This is one of the most requested features for ipfs-cluster.  ipfs-pack (https://github.com/ipfs/ipfs-pack) exists to fill a similar need by allowing data stored in a POSIX filesystem to be referenced from an ipfs-node's datastore without acutally copying all of the data into the node's repo.  However certain use cases require functionality beyond ipfs-pack. For example, what if a cluster of nodes needs to download a large file bigger than any of its nodes' individual machines' disk size?  In this case using ipfs-pack is not enough, such massive files would not fit on a single node's storage device and coordination between nodes to store the file becomes essential.

Storing large files is a feature that would serve many use cases, the more general ability to store large ipld dags of arbitrary structure has the potential to support others.  As an example consider importing a cryptocurrency ledger to an ipfs cluster when no one node could hold the entire dag.

## Motivation 2: store files with different replication strategies for fault tolerance
This feature is also frequently requested, including in the original thread leading to the creation of ipfs-cluster (See issue #1), and in @raptortech-js 's thorough discussion in issue #9.  The idea is to bring RAID modes of storage, including modes that incorporate space-efficient FEC techniques, to provide better fault tolerance of storage.  This would open up ipfs-cluster to new usage patterns, especially storing archives with infrequent updates and long storage periods.

Again, while files storage will benefit from efficient, fault tolerant encodings, these properties are potentially also quite useful for storing arbitrary merkle dags. 

# Basic sharding overview
## Problem scope and simplifying assumptions
The following takes key ideas from @hsanjuan's proposal in the comments reviewing PR #268.  To implement sharding, cluster must be able to split cids belonging to a single file among different cluster peers.  A first possible approach is to track the cid of every block of file data, however two complications present themselves.  First, if the file breaks down into many blocks then the cluster state risks growing too large tracking all the cids.  Second, under this approach cluster does not track the entire dag built by ipfs to store the file.  Ideally the entire dag can be somehow stored in ipfs cluster so that queries on cids in the dag receive the correct responses from the cluster, i.e. the same response that a single ipfs node storing the entire file would give.

Our approach at resolving these issues makes use of the following assumptions.  There is an ipld-dag that needs to be sharded, we assume:
1. The "data" nodes of the dag, i.e. the bulky nodes that needs sharding, are leaves
2. The rest of the dag, i.e. non-leaf nodes, can all fit within any single node's ipfs repo

Assuming the dag in question is an ipfs files dag, then 1 is satisfied by default.  We assume 2 throughout the rest of this section and the next.  We revist these assumptions and some possible sharding implementations that do not make them in the Complications section below.

## Implementation
### Overview
In brief the approach is to pin two dags in ipfs-cluster for every sharded file.  The first is the ipfs unixfs dag created upon adding the file to ipfs with one modification: its leaves are not attached.  Some subset and possibly all ipfs-cluster nodes pin this dag making use of the second assumption above and resolving the second complication above.  Additionally ipfs-cluster will collaboratively pin a "cluster-dag" specifically built to track and pin the leaves of the file dag in a way that reflects how data is grouped on cluster nodes.  It is organized in a 3 level tree.  The first level is the cluster-dag root, the second represents the shards into which data is divided with at least one shard per cluster peer.  The third level lists all the cids of leaves of the file dag included in a shard.  From @hsanjuan's original proposal:
```
The root would be like:
{
  "parts" : [
     {"/": <part1>},
     {"/": <part2>},
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
Each cluster node recursively pins a shard-node of the cluster-dag, ensuring that the blocks of file data referenced underneath the shard are pinned by that node.  The index of shards can be (non-recursively) pinned on any/all cluster nodes.

With this implementation the cluster can provide the entire original ipfs file dag on request.  For example if a node queries for the root of the original file dag they can get all non-leaf nodes from a single cluster peer.  The parents of the leaves will reference the leaf cids which cluster also provides, as they are all pinned somewhere as children of a shard-node in the cluster-dag.  Note this implementation avoids the first complication mentioned above because the cluster state need only keep track of 1 cid per shard and the root cid of the original dag, which prevents the state size from growing too large.

Under @hsanjuan's original proposal the state format would change slightly to account for linking together the root cid of an ipfs file dag and the cluster-dag pinning its leaves

```
"<cid>": {
  "name": <pinname>,
  "cid": <cid>,
  "cluserdag": <cidcluster>,
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

Under this implementation replication of shards proceeds like replication of an ordinary pinned cid.  If a shard is replicated across the cluster then data is not always lost during a node failure.  To get better guarantees for less extra space we will need to complete the work described in the fault tolerance section.


### Adding files
Currently the proposed implementation of basic sharding relies on ipfs-cluster having a lot of control over the process of adding a file to ipfs.  For now we assume that users must add data to be sharded to ipfs-cluster directly to give cluster this control over the insertion process, though later on cluster could ideally handle sharding existing dags using an ipld transform.  The cluster node adding the file will read in a stream of bytes that are then passed to an importer to create an ipfs files dag chunked (size-splitter or rabin-splitter) and layed out (trickle or balanced) per user instruction.  Concurrent with ipfs files dag creation, the importing ipfs-cluster node will oversee construction of a cluster-dag shard node that also references the data leaves of the ipfs files dag.  Once a shard is filled with leaf nodes it must be allocated and pinned to an ipfs-cluster node.  This process continues until both dags are fully constructed and pinned as expected.  There are a few potential mechanisms for transporting the shard from the importing node to the pinning node.  The importing node could add the shard to its ipfs node, pin, signal the pinning node to take over ownership (perhaps by adding to the state via consensus), and finally unpin the data.  Alternatively the importing node could `ipfs block put` and `ipfs pin` on the pinning node by making an RPC to the pinning node's cluster peer.  In this case perhaps the pinning node would check the state to ensure it was actually assigned the pin before running the rpc to completion (TODO -- I am still unclear exactly how consensus and shard transfer should work together, need to go through current implementation).  Note that this will be one of the more involved parts of implementing basic sharding and will almost certainly involve extracting, refactoring and adding features to the dag building functionality of go-ipfs.  See the Complications section for the additional possible hurdles for implementing this when the added file is too big to fit on one ipfs repo.


# Fault tolerance

## Background reading
This [report on RS coding for fault tolerance in RAID-like Systems by Plank](https://web.eecs.utk.edu/~plank/plank/papers/CS-96-332.pdf) is a very straightforward and practical guide.  It has been helpful in planning out how to approach fault tolerance in cluster and will be very helpful when we actually implement.  It has an excellent description of how to implement the algorithm that calculates the code from data, and how to implement recovery.  Furthermore one of his example use case studies include algorithms for initialization of the checksum values that will be useful when replicating very large files.

## Proposed implementation
### Overview
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

### Importing with checksums
If memory/repo size constraints are not a limiting factor it should be straightforward for the cluster-dag importer running on the adding node to keep a running tally of the checksum values and then allocate them to nodes after getting every data shard pinned.  Note this claim is specific to RS as the coding calculations are simple linear combinations of operations and everything commutes, while I wouldn't be surprised if potential future codes also had this property it is something we'll need to check up on once we get serious about pluggability.

If we are in a situation where shards approach the size of ipfs repo or working memory then we can gather inspiration from the report by Plank, specifically the section "Implementation and Performance Details: Checkpointing Systems".  In this section Plank outlines two algorithms for setting checksum values after the data shards are already stored by sending messages over the network.  From my first read-through the broadcast algorithm looks the most promising.  This algorithm would allow cluster to send shards one at a time to the peer holding a zeroed out checksum shard and then perform successive updates to calculate the checksums, rather than requiring that the cluster-dag importer hold the one shard being filled up for pinning alongside the m checksum shards being calculated.


### Automatic Recovery
TODO -- I have thoughts about implementing this in cluster that I need to organize and put down.

# Complications

## Files larger than local repos

### Improve cluster UX for large files
"How much room do I have in my cluster?" is a very predictable question that users importing huge files will want to know.  We should include cluster cli tools (something like ipfs metrics, or the still-unmade connectivity-graph) that inform users how much space they have available, and the largest recommended file the cluster can ingest.  Perhaps later on we can get even fancier and include predictions of insertion time, and a visualization of how many shards of what size will be added.

Although the ipfs add proxy endpoint is the logical place to launch the adding process we could also provide a user friendly command that takes in the option of a network address to add from.  This way cluster could properly manage the connection with a remote server while buffering and potentially blocking for long periods of time and prevent the user from having to do clever unix things to download large files.  Note I don't have much experience with this kind of thing, so maybe wget etc have really good UX for things like this and this would be overkill.

### Challenges adding files
When files become larger than local repos this becomes challenging for two reasons.  First, the importing node now needs to be able to block the importing process periodically to transfer data over to the pinning peer so that its local resources (ipfs repo and maybe memory) don't get overloaded.  Many related complications also seem likely.  For example if one shard takes up 90% of the node's ipfs repo and if the importing node makes use of its repo for temporary pinning and transfer then the importing node cannot pin its own shard until all other nodes have been served.  Second, I am unclear if either of the two proposed data transfer methods will provide acceptable performance when transferring large amounts of data (e.g. on the order of 10 GB shards).

The first challenge will require rethinking the go-ipfs dag building functionality and on the bright side may contribute to building a versatile foundation for the potential dagger/dex project.  The second challenge is best addressed by evaluating the performance characteristics of both data transfer methods.  Perhaps the ongoing ipfs metrics work will already answer many questions about the first method.  If the second transfer method (libp2p rpc) is a serious contender we should plan on evaluating its performance as part of implementation.  (TODO -- create an issue defining some metrics that we will want to evaluate) (TODO -- see if any existing ipfs metrics are relavent to first method, e.g. measuring remote pin time of dags with many large leaves)

### Collaborative importing
We could potentially use alternatives to importing the stream on one master node that then sends out shards to the assigned pinning nodes.  One other possibility is "collaborative importing", where the cluster node doing the "add" operation streams the raw data out to the pinning nodes that then do importing and pinning in place.

One potential downside is that importing the ipfs files dag becomes more complex, as there will in general be complex relationships between the dags of different shards. Note this should technically be possible, especially under the assumption that the entire ipfs file dag fits in all ipfs repos.  After each node runs the import they could combine together their ipfs dags (along with their data's relative order in the original stream) into a clever combining function that could construct the correct final graph on a single node.   This would also likely be more difficult to coordinate among nodes, and may be harder to get right in the face of network failures that require one node to restart downloading.  One final downside is that, depending on the splitter used, it may be difficult to efficiently stream data to peers that fits along the original chunk boundaries.

One upside is that the interaction between the ipfs files dag and cluster-dag importing process becomes much more simple.  The original importer could run to completion uninterrupted, and then the node could build the cluster-dag and unpin the leaves from the ipfs files dag.  Althought this wouldn't directly address the issues brought up above it is possible collaborative importing would result in a simpler download process as now downloads and imports are not interleaved.  It also seems likely that, if done right, collaborative importing could be faster overall, as importing would be parallelized across nodes.


## Arbitrary dags

TODO -- I have some thoughts on these (many voiced in my looong comment in response to @hsanjuan's proposal), I need to take some time to organize and record them here

### Dags too big to fit in ipfs repo

### Dags with data not in the leaves

### Dags already imported


# Workplan

There is a fair amount that needs to be built, updated and serviced to make all of this work.  Here I am focusing on basic sharding and allowing support for files that wouldn't fit in an ipfs repo.  Fault tolerance and arbitrary dag work should come after this is nailed down.  From current design fault tolerance will be straightforward on top of basic sharding and arbitrary dag support will probably be quite different and difficult regardless of how we implement the basics.

* specialized dag building tools 
  * cluster-dag ipld-format
  * cluster-dag importer
  * fundamental reimagining of go-ipfs dag building
    * Either ability to build multiple dags at once from two importers (single importer, most likely choice)
    * Or ability to combine together the dags of arbitrarily split leaves into the dag that would have arisen from importing all leaves at once (collaborative importers, less likely choice)
    * Could be the basis for an independent dagger library/tool
* metric sweeps
  * design metrics (rpc and/or ipfs remote pin unpin, and state size) and sweeps
  * if we want better performance then we can optimize (rpc) or build new transfer tools out of libp2p
* improving state component to scale (@hsanjuan from your proposal open questions)
* simple updates to state format to allow for cluster-dag support
* probably should work on improving migration framework as significant state changes are in the works
* adding functionality to ipfs-cluster add ipfs proxy endpoint
* changing configs and user commands to include `--shard` on add, in general figure out UX to determine when we shard
* potentially significant modifications to cluster peers signalling that a cid should be pinned (now they may be transferring data via rpc before hand) and so potentially significant modifications to how peers interact with state.  (@hsanjuan, this is something I haven't thought through enough to convince myself it won't be too different from now so I might be overstating the difficulty here)

