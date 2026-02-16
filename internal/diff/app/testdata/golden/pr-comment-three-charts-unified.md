<!-- chart-val-unified: my-app -->
## 📊 Helm Line Diff: `my-app`

✅ **Status:** Analysis complete — 2 environment(s) with changes

| Environment | Status |
|-------------|--------|
| `prod` | 📝 Changed |
| `staging` | 📝 Changed |

<details>
<summary><b>prod</b> — View diff</summary>

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

<details>
<summary><b>staging</b> — View diff</summary>

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

---
_Posted by chart-val_
