# Introduction
This document is a rough draft of an RFC for supporting sharded dags in ipfs-cluster.  After these ideas become more concrete, a little more thoroughly researched and have had time to incorporate changes suggested by ipfs-cluster contributors the goal is to include this RFC in ipfs/notes to get comments from the rest of the ipfs community.

This document outlines motivations for sharding dags across the nodes of an ipfs-cluster, and provides suggestions on development paths to achieve the motivated goals.  The "Thoughts on implementation" section describes development of sharding features.  It begins by making the strongest assumptions about the data, starting with familiar unixfs dags describing ipfs files that can be stored on any single node's repo, and working up to a discussion of arbitrary ipld dags that are too large to fit on any node's repo.

# Motivation
There are two primary motivations for adding data sharding to ipfs-cluster.  See the WIP use cases documents in PR #215, issue #212, and posts on the discuss.ipfs forum (ex: https://discuss.ipfs.io/t/ubuntu-archive-on-top-of-ipfs/1579 and https://discuss.ipfs.io/t/segmentation-file-in-ipfs-cluster/1465/2 and https://discuss.ipfs.io/t/when-file-upload-to-node-that-can-ipfs-segmentation-file-to-many-nodes/1451) for some more context.  (TODO -- investigate b5/data-together use cases and document whether this shows up there, if so add to use case doc + link here)

## Use case 1: store files too big for a single node
This is one of the most requested features for ipfs-cluster.  ipfs-pack (https://github.com/ipfs/ipfs-pack) exists to fill a similar need, as far as I understand it ipfs-pack allows data stored in a POSIX filesystem to be referenced from an ipfs-node's datastore without acutally copying all of the data into the nodes repo.  However certain use cases require functionality out of scope of ipfs-pack. For example, what if a cluster needs to download a large file bigger than any of its nodes individual machines' disk size?  Supporting functionality for adding this file to ipfs across cluster nodes in ipfs-cluster would provide a simple path for adding large files to ipfs.  Even if the large file is stored locally on a storage device accessible to a cluster node's machine, and therefore could be referenced by ipfs-pack, users may want to keep data directly in ipfs repos.  (TODO -- evaluate this more precisely, are there specific instances where this is the case, ex, discoverability and latency issues with ipfs-pack? Would improving ipfs-pack / datastore integration better serve users in these cases?).  Additionally even using ipfs-pack, truly massive files might still not be able to fit on a single storage device on an ipfs node and in this case coordination between nodes to store the file becomes essential.

Although storing large files is a natural feature that would serve many use cases, the more general ability to store large ipld dags of arbitrary structure potentially supports more use cases, for example importing a cryptocurrency ledger to an ipfs cluster, of which no one node could hold the entire dag.

## Use case 2: store files with different replication strategies for fault tolerance
This use case is brought up from time to time, including in the original thread leading to the creation of ipfs-cluster (See issue #1), and in @raptortech-js 's thorough discussion in issue #9.  As I understand it the idea is to bring RAID modes of storage (including modes that incorporate sophisticated FEC techniques) to provide better fault tolerance for a given space efficiency of storage.  This would open up ipfs-cluster to new usage patterns, especially storing archives with infrequent reads and indefinite storage periods.

Again, while files storage could benefit from efficient, fault tolerant encodings, these properties are potentially desirable for the long term storage of arbitrary merkle dags as well. 

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











