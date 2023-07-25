local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.billing_collector_cloudservices;
local kube = import 'lib/kube.libjsonnet';
local com = import 'lib/commodore.libjsonnet';
local collectorImage = '%(registry)s/%(repository)s:%(tag)s' % params.images.collector;
local component_name = 'billing-collector-cloudservices';


local labels = {
  'app.kubernetes.io/name': component_name,
  'app.kubernetes.io/managed-by': 'commodore',
  'app.kubernetes.io/part-of': 'appuio-cloud-reporting',
  'app.kubernetes.io/component': component_name,
};

local secret(key) = [
  if params.secrets[key][s] != null then
    kube.Secret(s + '-' + key) {
      metadata+: {
        namespace: params.namespace,
      },
    } + com.makeMergeable(params.secrets[key][s])
  for s in std.objectFields(params.secrets[key])
];

local healthProbe = {
  httpGet: {
    path: '/metrics',
    port: 9123,
  },
  periodSeconds: 30,
};

local exoClusterRole = kube.ClusterRole('appcat:cloudcollector:exoscale') + {
  rules: [
    {
      apiGroups: [ '*' ],
      resources: [ 'namespaces' ],
      verbs: [ 'get', 'list' ],
    },
    {
      apiGroups: [ 'exoscale.crossplane.io' ],
      resources: [
        'buckets',
        'postgresqls',
        'mysqls',
        'redis',
        'opensearches',
        'kafkas',
      ],
      verbs: [
        'get',
        'list',
        'watch',
      ],
    },
  ],
};

local cloudscaleClusterRole = kube.ClusterRole('appcat:cloudcollector:cloudscale') + {
  rules: [
    {
      apiGroups: [ '' ],
      resources: [ 'namespaces' ],
      verbs: [ 'get', 'list' ],
    },
    {
      apiGroups: [ 'cloudscale.crossplane.io' ],
      resources: [
        'buckets',
      ],
      verbs: [
        'get',
        'list',
        'watch',
      ],
    },
  ],
};

local serviceAccount(name, clusterRole) = {
  local sa = kube.ServiceAccount(name) + {
    metadata+: {
      namespace: params.namespace,
    },
  },
  local rb = kube.ClusterRoleBinding(name) {
    roleRef_: clusterRole,
    subjects_: [ sa ],
  },
  sa: sa,
  rb: rb,
};

local deployment(name, args) =
  kube.Deployment(name) {
    metadata+: {
      labels+: labels,
      namespace: params.namespace,
    },
    spec+: {
      template+: {
        spec+: {
          serviceAccount: name,
          containers_:: {
            exporter: kube.Container('exporter') {
              imagePullPolicy: 'IfNotPresent',
              image: collectorImage,
              args: args,
              envFrom: [
                {
                  secretRef: {
                    name: 'credentials-' + name,
                  },
                },
              ],
              ports_:: {
                exporter: {
                  containerPort: 9123,
                },
              },
              readinessProbe: healthProbe,
              livenessProbe: healthProbe,
            },
          },
        },
      },
    },
  };

local podMonitor(name) = kube._Object('monitoring.coreos.com/v1', 'PodMonitor', 'hello') + {
  metadata: {
    name: name + '-podmonitor',
    namespace: params.namespace,
  },
  spec: {
    podMetricsEndpoints: [
      {
        port: 'exporter',
      },
    ],
    selector: {
      matchLabels: {
        name: name,
      },
    },
  },
};

local promRule = kube._Object('monitoring.coreos.com/v1', 'PrometheusRule', 'appcat-cloud-billing') {
  metadata+: {
    namespace: params.namespace,
  },
  spec: {
    groups: [
      {
        name: 'appcat:billing:cloudservices',
        rules: [
          {
            expr: 'max_over_time(appcat:raw:billing{type="dbaas"}[1h])',
            record: 'appcat:billing',
          },
          {
            expr: 'appcat:raw:billing{type!="dbaas"}',
            record: 'appcat:billing',
          },
        ],
      },
    ],
  },
};

local orgOverride = (
  if params.appuioManaged
  then
    [ '--organizationOverride', params.tenantID ]
  else
    []
);

(if params.exoscale.enabled || params.cloudscale.enabled then {
   promRule: promRule,
 } else {}) +
(if params.exoscale.enabled then {
   local secrets = params.secrets.exoscale,
   local intervall = std.toString(params.exoscale.intervall),
   local name = 'exoscale',
   local sa = serviceAccount(name, exoClusterRole),
   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_KEY != null : 'secrets.credentials.stringData.EXOSCALE_API_KEY must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_SECRET != null : 'secrets.credentials.stringData.EXOSCALE_API_SECRET must be set.',

   exoSecrets: std.filter(function(it) it != null, secret(name)),
   exoPodMonitor: podMonitor(name),
   exoClusterRole: exoClusterRole,
   exoServiceAccount: sa.sa,
   exoRoleBinding: sa.rb,
   exoscaleExporter: deployment(name, orgOverride + [ '--collectInterval', intervall, name ]),
 } else {})
+
(if params.cloudscale.enabled then {
   local secrets = params.secrets.cloudscale,
   local intervall = std.toString(params.cloudscale.intervall),
   local name = 'cloudscale',
   local sa = serviceAccount(name, cloudscaleClusterRole),
   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.CLOUDSCALE_API_TOKEN != null : 'secrets.credentials.stringData.CLOUDSCALE_API_TOKEN must be set.',

   cloudscaleSecrets: std.filter(function(it) it != null, secret(name)),
   cloudscalePodMonitor: podMonitor(name),
   cloudscaleClusterRole: cloudscaleClusterRole,
   cloudscaleServiceAccount: sa.sa,
   cloudscaleRolebinding: sa.rb,
   cloudscaleExporter: deployment(name, orgOverride + [ '--collectInterval', intervall, name ]),
 } else {})
