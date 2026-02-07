# chart-sentry: my-app/prod

**Status:** completed
**Conclusion:** neutral

## Helm diff — my-app (prod)

### Summary
Changes detected in my-app for environment prod.

### Output
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
