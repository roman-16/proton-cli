export interface Endpoint {
  name: string;
  method: string;
  url: string;
  tag: string;
  description: string;
  deprecated: boolean;
  isPublic: boolean;
  pathParams: string[];
  queryParams: Property[];
  bodyParams: Property[];
  hasBody: boolean;
  inputType: string;
  outputType: string;
  timeout: string;
  keepalive: boolean;
  silencedErrors: string[];
}

export interface Property {
  name: string;
  type: string;
  optional: boolean;
  description: string;
}

export interface EnumInfo {
  name: string;
  values: { key: string; value: string | number }[];
}
