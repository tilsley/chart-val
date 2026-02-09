# chart-val: new-chart

**Status:** completed
**Conclusion:** success

## Helm diff — new-chart

### Summary
Analyzed 1 environment(s): 1 changed, 0 unchanged

### Output
<details><summary>prod — Changed</summary>

**Semantic Diff (dyff):**
```diff
--- new-chart/prod (main)
+++ new-chart/prod (feat/add-new-chart)

(root level)
+ four map entries added:
  # Source: new-chart/templates/deployment.yaml
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: new-chart
  spec:
    replicas: 3
    selector:
      matchLabels:
        app: new-chart
    template:
      metadata:
        labels:
          app: new-chart
      spec:
        containers:
        - name: new-chart
          image: "new-chart:1.0.0"
          ports:
          - containerPort: 8080
```

**Unified Diff (line-based):**
```diff
--- new-chart/prod (main)
+++ new-chart/prod (feat/add-new-chart)
@@ -1 +1,22 @@
+---
+# Source: new-chart/templates/deployment.yaml
+apiVersion: apps/v1
+kind: Deployment
+metadata:
+  name: new-chart
+spec:
+  replicas: 3
+  selector:
+    matchLabels:
+      app: new-chart
+  template:
+    metadata:
+      labels:
+        app: new-chart
+    spec:
+      containers:
+        - name: new-chart
+          image: "new-chart:1.0.0"
+          ports:
+            - containerPort: 8080
```

</details>
