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



# Complications

## Files larger than local repos

### Improve cluster UX for large files
"How much room do I have in my cluster?" is a very predictable question that users importing huge files will want to know.  We should include cluster cli tools (something like ipfs metrics, or the still-unmade connectivity-graph) that inform users how much space they have available, and the largest recommended file the cluster can ingest.  Perhaps later on we can get even fancier and include predictions of insertion time, and a visualization of how many shards of what size will be added.

Although the ipfs add proxy endpoint is the logical place to launch the adding process we could also provide a user friendly command that takes in the option of a network address to add from.  This way cluster could properly manage the connection with a remote server while buffering and potentially blocking for long periods of time and prevent the user from having to do clever unix things to download large files.  Note I don't have much experience with this kind of thing, so maybe wget etc have really good UX for things like this and this would be overkill.

### Challenges adding files
When files become larger than local repos this becomes challenging for two reasons.  First, the importing node now needs to be able to block the importing process periodically to transfer data over to the pinning peer so that its local resources (ipfs repo and maybe memory) don't get overloaded.  Many related complications also seem likely.  For example if one shard takes up 90% of the node's ipfs repo and if the importing node makes use of its repo for temporary pinning and transfer then the importing node cannot pin its own shard until all other nodes have been served.  Second, I am unclear if either of the two proposed data transfer methods will provide acceptable performance when transferring large amounts of data (e.g. on the order of 10 GB shards).

The first challenge will require rethinking the go-ipfs dag building functionality and on the bright side may contribute to building a versatile foundation for the potential dagger/dex project.  The second challenge is best addressed by evaluating the performance characteristics of both data transfer methods.  Perhaps the ongoing ipfs metrics work will already answer many questions about the first method.  If the second transfer method (libp2p rpc) is a serious contender we should plan on evaluating its performance as part of implementation.  (TODO -- create an issue defining some metrics that we will want to evaluate) (TODO -- see if any existing ipfs metrics are relavent to first method, e.g. measuring remote pin time of dags with many large leaves)

### Collaborative importing
We could potentially use alternatives to importing the stream on one master node that then sends out shards to the assigned pinning nodes.  One other possibility is collaborative impori


## Arbitrary dags


# Sketch of development plans





# Thoughts on implementation
This section takes a stab at planning out on a high level what needs to be done to fully support the two use cases above.  It progresses from strong assumptions and easier implementation to fewer assumptions and more difficult implementation paths.  For example collaborative file importing might be the most useful feature to come out of this work, but from my perspective it is going to be challenging to build and so we should focus first on laying a foundation.  Similarly, supporting fault tolerant storage of immense arbitrary merkle dags in ipfs-cluster would be very useful, but since the mechanism of storing non-unixfs ipld dags isn't even, AFAIK, nailed down yet, our time is better spent focusing on what ipfs can readily support.  

## Small dags
Note, by small dags, I mean a dag that can fit within every node of the clusters' repo without sharding.  

### Starting point: RAID 0 replication with small unixfs files
Refresher: RAID 0 takes a file consisting of multiple blocks and puts different chunks on different disks (in our case ipfs repos).  No parity information is stored.  For ipfs-cluster storing a file with multiple chunks under RAID 0 replication causes the individual blocks to be allocated across separate ipfs nodes.  Note, replication is a misnomer as data does not appear more than once in any repo under RAID 0 mode.

Starting with RAID 0 allows us to focus on supporting sharded file support as a foundation without having to implement support for parity aware chunking algorithms that will be necessary for fault tolerant replication.  Focusing on files that result in small dags greatly simplifies the task, as now we can assume that the file's dag exists in entirety on a node of the cluster before allocating pinning

#### Distributing data
Under RAID 0 replication assigning storage to a file happens in the same general manner as it does now, an allocator making use of some metrics or policy makes the decision, updates the state to reflect this, and upon reaching consensus the nodes of the cluster affected by the new allocation update their local state.  The difference now is that a single pinned file causes, in general, multiple ipfs nodes to update their pinsets.  Under the current workflow model the pinning node will have the file added to its ipfs repo and recall that cluster nodes are all swarm peers.  In light of the fact that ipfs nodes act as providers of every block of every file pinned, nodes that are assigned to pin one of the file blocks after the RAID 0 replicated pin makes it through consensus can do so with a simple `ipfs pin <cid-of-block>` call to their local ipfs endpoint.  Possibly the orignal pinning node can subsequently unpin blocks to make room in its repo, though this will require further coordination, probably through the consensus protocol. 
With new additions to the set of possible replication strategies and the recent modularity given to cluster configs, it is likely that we will want to start giving users the option to specify replication policies in one of the component configs, perhaps the allocator?  Potentially we may want a new component for replication related functionality too.

TODO -- More thought should go into pinning the part of the dag defining the layout (e.g. the non leaf nodes).  Should these be pinned in a single node? Across mulitple, all? Should this be determined by a parameter in the RAID 0 replication strategy?

TODO -- The state format will need to change to allow for these RAID 0 replicated pins.  This should be relatively straightforward but deserves a little more thought.

#### Parsing the meta-data describing file-dag layout
When pinning a file in RAID 0 mode, ipfs-cluster begins by seeking out the cids of the individual chunks that make up this file so that they can be assigned to peers by the allocator.  There are a few details here.

Getting the leaf hashes, e.g. the data block hashes, may be simple under the current ipfs api, or may require some interactive dag expansion on the part of the ipfs-cluster process looking for the cids (TODO -- investigate this more thoroughly).  Once ipld selectors are incorporated into the ipfs dag framework they could also be a way to easily enumerate all of the leaf cids.

Coming back to a point made in the earlier section, more thought needs to go into how to handle the non-leaf parts of the dag.  I can see a few possibilities here.  We can think about storing this separately from the actual file data (the leaves) on some subset of the nodes.  We could also store subgraphs of the dag more completely when doing allocations.  For example a file tree with sibling width of n and n ipfs nodes could pin the n top most subtrees one per node.  Of course there will always be at least *one* node of the dag that references information stored on multiple nodes that we need to figure out how to replicate across the cluster.  To get a file dag into a state where it is simple to evenly break it up across exactly the number of nodes in the cluster we could potentially leverage a new customized layout that is aware of the total number of subdags into which we split the dag. Doing something like this would require either a user who does the original `ipfs add` aware of this layout requirement, a workflow where users primarily call `ipfs add` against the ipfs-cluster vnode interface when adding to cluster (i.e. cluster adds in some layout magic when forwarding the proxy), or the use of an ipld transformation (currently fairly far from being implemented) (TODO -- everything in this paragraph needs more thought and eventually a few decisions)

#### Interface and work flow discussion
So far this section has only addressed adding a file and pinning it with RAID 0 mode replication.  In reality RAID 0 mode will probably not be needed for most use cases, in our case it is a stepping stone on the way towards more sophisticated features.  However we should start considering the entire workflow that users of ipfs-cluster will want when interacting with replication-strategy pinned data more generally.  In the case of RAID 0 things are particularly simple because, excluding some discussion about changing dag layout when talking about encorporating meta-data dag nodes, the file dag generated by adding the file to a single ipfs node is equivalent to the dag generated by replicating the file across the cluster.  Supporting more complicated RAID modes quickly leads to the question of how aware the pinning node should be of the underlying replication strategy, and therefore the cids of the dag actually stored across nodes.  One could image a mapping between canonically built dag cids and RAID-specific dag cids so that users remain unaware of the storage details of data added with different replication modes.  On the flip side it is possible users want to know the exact hashes of dags generated under specific replication modes.

TODO -- discussion on what kind of interfaces we want.  Are we trying to accentuate one or the other of 1) opaque "vnode" interface 2) cluster-aware interfaces (like current pinning).  Currently both exist in some form, though 2) seems favored by current work flows and 1) seems closer to the original vision discussed in issue #1.

One requirement of moving much farther forward is to pin down more precisely the querying workflows we are looking to support for various features.  This will most likely be fairly use case dependent, but we are going to need to design our interfaces around this question (it should help us answer the above TODO).

TODO -- further investigation into querying workflows, more time spent understanding certain use cases.  As far as I know the current querying is all done by discovering content on the ipfs nodes in the cluster.  Is this all we are trying to support?  What about giving the cluster ipfs proxy address as an ipfs address?  Are we thinking about searching (and hence indexing) cluster content? There is a lot here that I don't know.

#### Supporting more complex unixfs datastructures
- files are described above, now what about directories?
- TODO -- research why the wikipedia mirror needs the experimental sharded directory feature, how this works, and if there is special functionality in ipfs that we need to use to get files sharded (don't think so but good to check)

### Fault tolerant RAID replication modes with small unixfs files
- Big difference is that data needs a transformation before storage
- a few places this could go, custom chunker, custom dag layout
- requires we answer some questions about how aware users are of the replication format
- drives home the point that allocators need a lot of awareness regarding the dag they are pinning, for RAID 4/5 the parity blocks need to be treated separately (TODO -- understand more about this).  Using FEC we actually have an easier time because parity distributes with every block
- Since we are storing parity checking/correction information that leads to the natural question of how cluster will support reconstructing data that is in error.  Figuring this out is related to the interface and workflow questions; how are users going to fetch data?  block by block? all at once?
- ipfs-cluster can and should also make use of fault tolerant replication of data in the event of a node failure.  In this case the missing data block can be reconstructed (potentially this is going to need all of the other blocks for a big slowdown) to create the missing block from the existing ones and then repin.
- Thinking that a lot of RAID modes will not be useful, depending on whether they just check for corruption, because ipfs does integrity checks on fetch

### Fault tolerant RAID replication modes with small general dags
- whereas above the question is how to modify the importing process (chunker, specialized dag layout), now the question is more about ipld transformations.  A huge TODO is figuring out exactly where the replication parity information should go to transform a dag into an error correcting or more fault tolerant dag.
- this is somewhat getting ahead of itself because ipfs currently doesn't support adding ipld dags to the repo.  Sharding dags for fault tolerance may not have a motivating use case depending on the extent to which ipld dag storage is supported.  Also possible that unforseen cleverness in the dag storage model might make fault tolerant sharding of dags less useful than I am expecting.
- important to follow along with the CAR format's development to make sense of how this might all fit together

## Large dags

### Collaborative importing



### Arbitrary dag considerations











