# High Availability Single Node Openshift Setup Operator

Provide the ability for exactly two Single Node OpenShift clusters to operate as a **predefined** pair in an **active-passive** or **active-active** configuration, detect when its peer has died and **automatically** take over its workloads after ensuring it is safe to do so.

## Motivation

Some companies have a need for a highly available container management solution that fits within a reduced footprint.
- The hardware savings are significant for customers deploying many remote sites (kiosks, branch offices, restaurant chains, etc), most notably for edge computing and RAN specifically.
- The physical constraints of some deployments prevent more than two nodes (planes, submarines, satellites, and also RAN).
- Some locations will not have reliable network connections or limited bandwidth (once again submarines, satellites, and disaster areas such as after hurricanes or floods)

## Pre-requisites
* 2 SNO Clusters.
* Deployments that will be managed by HA Layer already exist.

## Installation
* Deploy This operator to each SNO cluster.
* Load the yaml manifest of the HASNO (for each SNO cluster).

## Assumptions
* CRs will be updated simultaneously on both clusters by a config management tool - for example ACM (in case no such tool is used it should be done manually by the user).

## Example CRs
An example HASNO object.
```yaml
   apiVersion: app.hasno.com/v1alpha1
   kind: HALayerSet
   metadata:
     name: halayerset-sample
   spec:
     # Add fields here
     deployments:
       - "nginx-test"
       - "nginx-prod"
     fenceAgentsSpec:
     - name: "fence_ipmilan_1"
       type: "fence_ipmilan"
       params:
         ip: "192.168.126.1"
         username: "admin"
         password: "password"
         ipport: "9111"
         lanplus: "1"
         pcmk_host_list: "cluster1"
     - name: "fence_ipmilan_2"
       type: "fence_ipmilan"
       params:
         ip: "192.168.126.1"
         username: "admin"
         password: "password"
         ipport: "9222"
         lanplus: "1"
         pcmk_host_list: "cluster2"
     nodesSpec:
       firstNodeName: "cluster1"
       firstNodeIP: "192.168.126.10"
       secondNodeName: "cluster2"
       secondNodeIP: "192.168.126.11"
```
These CRs are created by the admin and are used to trigger the setting of the High Availability Layer on top of the SNO clusters.