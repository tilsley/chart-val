# chart-val

**Status:** completed
**Conclusion:** success

## Helm Diff

### Summary
Analyzed 3 chart(s): 1 with changes, 2 unchanged

### Output
## my-app

<details><summary>prod — Changed</summary>

**Semantic Diff (dyff):**
```diff
--- my-app/prod (main)
+++ my-app/prod (feat/update-config)

metadata.labels.version  (apps/v1/Deployment/my-app)
  ± value change
    - 0.1.0
    + 0.2.0

spec.replicas  (apps/v1/Deployment/my-app)
  ± value change
    - 3
    + 5

spec.template.spec.containers.my-app.image  (apps/v1/Deployment/my-app)
  ± value change
    - my-app:1.24.0
    + my-app:1.25.0

spec.template.spec.containers.my-app.env  (apps/v1/Deployment/my-app)
  + one list entry added:
    - name: ENABLE_CACHE
      value: "true"

spec.template.spec.containers.my-app.resources.limits.cpu  (apps/v1/Deployment/my-app)
  ± value change
    - 1
    + 2

spec.template.spec.containers.my-app.resources.limits.memory  (apps/v1/Deployment/my-app)
  ± value change
    - 1Gi
    + 2Gi

(root level)  (v1/ConfigMap/my-app-config)
+ one document added:
  ---
  # Source: my-app/templates/configmap.yaml
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: my-app-config
    labels:
      app: my-app
  data:
    LOG_LEVEL: warn
    CACHE_ENABLED: "true"
```

**Unified Diff (line-based):**
```diff
--- my-app/prod (main)
+++ my-app/prod (feat/update-config)
@@ -1,3 +1,14 @@
+---
+# Source: my-app/templates/configmap.yaml
+apiVersion: v1
+kind: ConfigMap
+metadata:
+  name: my-app-config
+  labels:
+    app: my-app
+data:
+  LOG_LEVEL: warn
+  CACHE_ENABLED: "true"
 ---
 # Source: my-app/templates/service.yaml
 apiVersion: v1
@@ -22,9 +33,9 @@
   name: my-app
   labels:
     app: my-app
-    version: 0.1.0
+    version: 0.2.0
 spec:
-  replicas: 3
+  replicas: 5
   selector:
     matchLabels:
       app: my-app
@@ -35,17 +46,19 @@
     spec:
       containers:
         - name: my-app
-          image: "my-app:1.24.0"
+          image: "my-app:1.25.0"
           ports:
             - containerPort: 80
           env:
             - name: LOG_LEVEL
               value: warn
+            - name: ENABLE_CACHE
+              value: "true"
           resources:
             requests:
               cpu: 500m
               memory: 512Mi
             limits:
-              cpu: 1
-              memory: 1Gi
+              cpu: 2
+              memory: 2Gi
```

</details>

<details><summary>staging — Changed</summary>

**Semantic Diff (dyff):**
```diff
--- my-app/staging (main)
+++ my-app/staging (feat/update-config)

metadata.labels.version  (apps/v1/Deployment/my-app)
  ± value change
    - 0.1.0
    + 0.2.0

spec.replicas  (apps/v1/Deployment/my-app)
  ± value change
    - 2
    + 3

spec.template.spec.containers.my-app.image  (apps/v1/Deployment/my-app)
  ± value change
    - my-app:1.24.0
    + my-app:1.25.0

spec.template.spec.containers.my-app.env  (apps/v1/Deployment/my-app)
  + one list entry added:
    - name: ENABLE_CACHE
      value: "true"

(root level)  (v1/ConfigMap/my-app-config)
+ one document added:
  ---
  # Source: my-app/templates/configmap.yaml
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: my-app-config
    labels:
      app: my-app
  data:
    LOG_LEVEL: debug
    CACHE_ENABLED: "true"
```

**Unified Diff (line-based):**
```diff
--- my-app/staging (main)
+++ my-app/staging (feat/update-config)
@@ -1,3 +1,14 @@
+---
+# Source: my-app/templates/configmap.yaml
+apiVersion: v1
+kind: ConfigMap
+metadata:
+  name: my-app-config
+  labels:
+    app: my-app
+data:
+  LOG_LEVEL: debug
+  CACHE_ENABLED: "true"
 ---
 # Source: my-app/templates/service.yaml
 apiVersion: v1
@@ -22,9 +33,9 @@
   name: my-app
   labels:
     app: my-app
-    version: 0.1.0
+    version: 0.2.0
 spec:
-  replicas: 2
+  replicas: 3
   selector:
     matchLabels:
       app: my-app
@@ -35,12 +46,14 @@
     spec:
       containers:
         - name: my-app
-          image: "my-app:1.24.0"
+          image: "my-app:1.25.0"
           ports:
             - containerPort: 80
           env:
             - name: LOG_LEVEL
               value: debug
+            - name: ENABLE_CACHE
+              value: "true"
           resources:
             requests:
               cpu: 100m
```

</details>

## Unchanged charts

The following charts were analyzed and had no changes across all environments:

- `stable-app`
- `another-app`

